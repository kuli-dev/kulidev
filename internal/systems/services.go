package systems

import (
	"bytes"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/kuli-dev/kulidev/internal/store"
)

// Services are what actually gets deployed and run: docker-compose services,
// Kubernetes workloads and Docker images. One repository can run several (an API and
// its workers from the same code), and one service is often defined once per
// environment (k8s/dev, k8s/prod, …): those definitions are merged.

// Service kinds.
const (
	KindAPI      = "api"      // serves HTTP or exposes a port
	KindWorker   = "worker"   // long-running, no port
	KindCron     = "cronjob"  // runs on a schedule
	KindJob      = "job"      // runs to completion
	KindFrontend = "frontend" // a web front end
	KindDatabase = "database" // backing services from a stock image
	KindCache    = "cache"
	KindQueue    = "queue"
	KindStorage  = "storage"
	KindProxy    = "proxy"
)

// Service is one deployable unit.
type Service struct {
	Name     string     `json:"name"`
	Kind     string     `json:"kind"`
	Project  string     `json:"project"`            // project path whose code it runs ("" unknown or a stock image)
	Deployed string     `json:"deployed,omitempty"` // project path the deploy file is in, when different
	Image    string     `json:"image,omitempty"`
	Ports    []int      `json:"ports"`
	Command  string     `json:"command,omitempty"`
	Envs     []string   `json:"envs"`    // where it is defined to run: compose, dev, prod, …
	Via      []string   `json:"via"`     // docker-compose, kubernetes, dockerfile
	Sources  []Evidence `json:"sources"` // definitions, file:line
	aliases  []string   // other names it is reached by (k8s Service names, compose aliases)
}

// ServiceLink is one service using another.
type ServiceLink struct {
	From     string     `json:"from"`
	To       string     `json:"to"`
	Kind     string     `json:"kind"` // "depends on" (compose), "calls" (a URL in its config names the other)
	Evidence []Evidence `json:"evidence"`
}

// Stock images: backing services rather than the system's own code.
var backing = []struct {
	re   *regexp.Regexp
	kind string
}{
	{regexp.MustCompile(`(^|/)(postgres|postgis|timescale|mysql|mariadb|mongo|cockroach|clickhouse|cassandra|neo4j|mssql|sqlserver|pgvector|supabase)`), KindDatabase},
	{regexp.MustCompile(`(^|/)(redis|valkey|memcached|keydb|dragonfly)`), KindCache},
	{regexp.MustCompile(`(^|/)(rabbitmq|kafka|zookeeper|nats|activemq|redpanda|pulsar|mosquitto|emqx)`), KindQueue},
	{regexp.MustCompile(`(^|/)(minio|localstack|azurite|elasticsearch|opensearch|qdrant|weaviate|milvus|chroma|meilisearch|typesense)`), KindStorage},
	{regexp.MustCompile(`(^|/)(nginx|traefik|caddy|haproxy|envoy|kong)`), KindProxy},
}

func backingKind(image string) string {
	img := strings.ToLower(image)
	if i := strings.LastIndex(img, "@"); i > 0 {
		img = img[:i]
	}
	for _, b := range backing {
		if b.re.MatchString(img) {
			return b.kind
		}
	}
	return ""
}

var frontendDeps = regexp.MustCompile(`"(next|react-scripts|vite|@angular/core|vue|nuxt|svelte|@sveltejs/kit|gatsby|@remix-run/react|astro)"\s*:`)

type svcBuilder struct {
	b       *builder
	pending []pendingLink // depends_on, resolved once every service is known
	byName  map[string]*Service
	order   []string
	links   map[[3]string]*ServiceLink
	served  map[int]bool // projects whose code defines HTTP routes
}

func (b *builder) services(snap *store.Snapshot) ([]Service, []ServiceLink) {
	sb := &svcBuilder{b: b, byName: map[string]*Service{}, links: map[[3]string]*ServiceLink{}, served: b.served}
	compose, kube := map[string]bool{}, map[string]bool{}
	for kind, paths := range snap.Manifests {
		for _, p := range paths {
			switch kind {
			case "Docker Compose":
				compose[p] = true
			case "Kubernetes manifest":
				kube[p] = true
			}
		}
	}
	for _, p := range sortedSet(compose) {
		sb.compose(p)
	}
	for _, p := range sortedSet(kube) {
		sb.kubernetes(p)
	}
	sb.dockerfiles(snap)
	for _, l := range sb.pending {
		sb.link(l.from, l.to, "depends on", l.ev)
	}
	sb.calls(compose, kube)

	out := make([]Service, 0, len(sb.order))
	for _, n := range sb.order {
		s := sb.byName[n]
		sort.Strings(s.Envs)
		sort.Ints(s.Ports)
		if s.Ports == nil {
			s.Ports = []int{}
		}
		if s.Envs == nil {
			s.Envs = []string{}
		}
		out = append(out, *s)
	}
	links := make([]ServiceLink, 0, len(sb.links))
	for _, l := range sb.links {
		links = append(links, *l)
	}
	sort.Slice(links, func(i, j int) bool {
		return links[i].From+links[i].To+links[i].Kind < links[j].From+links[j].To+links[j].Kind
	})
	return out, links
}

func sortedSet(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// add merges a definition into the service of that name.
func (sb *svcBuilder) add(name string, def Service) *Service {
	s := sb.byName[name]
	if s == nil {
		def.Name = name
		sb.byName[name] = &def
		sb.order = append(sb.order, name)
		return &def
	}
	if s.Kind == "" || s.Kind == KindWorker && def.Kind != "" {
		s.Kind = def.Kind
	}
	if s.Project == "" {
		s.Project = def.Project
	}
	if s.Image == "" {
		s.Image = def.Image
	}
	if s.Command == "" {
		s.Command = def.Command
	}
	for _, p := range def.Ports {
		if !containsInt(s.Ports, p) {
			s.Ports = append(s.Ports, p)
		}
	}
	s.Envs = union(s.Envs, def.Envs)
	s.Via = union(s.Via, def.Via)
	if len(s.Sources) < 12 {
		s.Sources = append(s.Sources, def.Sources...)
	}
	s.aliases = union(s.aliases, def.aliases)
	return s
}

func union(a, b []string) []string {
	for _, x := range b {
		if x != "" && !contains(a, x) {
			a = append(a, x)
		}
	}
	return a
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func containsInt(list []int, n int) bool {
	for _, x := range list {
		if x == n {
			return true
		}
	}
	return false
}

// projectOfDir is the project a folder (relative to the root) belongs to.
func (sb *svcBuilder) projectOfDir(dir string) string {
	if i := sb.b.of(path.Join(dir, "x")); i >= 0 {
		return sb.b.projects[i].Path
	}
	return ""
}

// codeProject guesses whose code an image runs: the project whose name it contains,
// else the project of the file that deploys it.
func (sb *svcBuilder) codeProject(image, name, deployedFrom string) string {
	hay := strings.ToLower(image + " " + name)
	best, bestLen := "", 0
	for _, p := range sb.b.projects {
		n := strings.ToLower(p.Name)
		if p.Path == "" || len(n) < 4 {
			continue
		}
		for _, v := range []string{n, strings.ReplaceAll(n, "_", "-"), strings.ReplaceAll(n, "-", "_")} {
			if strings.Contains(hay, v) && len(v) > bestLen {
				best, bestLen = p.Path, len(v)
			}
		}
	}
	if best != "" {
		return best
	}
	return deployedFrom
}

var envName = regexp.MustCompile(`(?i)^(.*[-_])?(dev|develop|development|staging|stage|stg|prod|production|prd|qa|test|uat|preview|demo|sandbox|local|live)([-_].*)?$`)

// env names the environment a Kubernetes definition is for, from its folder
// (k8s/dev, overlays/prod, demo-eu, …); folders that aren't environments (base,
// cronjobs, …) give "".
func envOf(file string) string {
	for d := path.Dir(file); d != "." && d != "/"; d = path.Dir(d) {
		if b := path.Base(d); envName.MatchString(b) {
			return b
		}
	}
	return ""
}

var (
	workerName   = regexp.MustCompile(`(?i)(consumer|worker|scheduler|queue|listener|processor|pipeline|sync|beat|celery)`)
	frontendName = regexp.MustCompile(`(?i)(frontend|front-end|(^|[-_])(web|ui|spa|portal|dashboard)($|[-_]))`)
)

// kindFor: stock images are backing services; then a front end, a worker (by its
// name: a port on a consumer is usually a health check), or an API if it listens.
func (sb *svcBuilder) kindFor(image string, ports []int, name string, frontend bool) string {
	if k := backingKind(image); k != "" {
		return k
	}
	if frontend || frontendName.MatchString(name) {
		return KindFrontend
	}
	if workerName.MatchString(name) {
		return KindWorker
	}
	if len(ports) > 0 {
		return KindAPI
	}
	return KindWorker
}

// isFrontendDir: a web front end is built from dir (its package.json uses a front-end
// framework, and no backend project file sits beside it).
func (sb *svcBuilder) isFrontendDir(dir string) bool {
	join := func(f string) string {
		if dir == "" || dir == "." {
			return f
		}
		return dir + "/" + f
	}
	if !frontendDeps.Match(sb.b.read(join("package.json"))) {
		return false
	}
	for _, m := range []string{"pyproject.toml", "go.mod", "setup.py", "pom.xml", "composer.json"} {
		if sb.b.read(join(m)) != nil {
			return false
		}
	}
	return true
}

func (sb *svcBuilder) byProject(p string) int {
	for i, x := range sb.b.projects {
		if x.Path == p {
			return i
		}
	}
	return -1
}

// ---- docker-compose ------------------------------------------------------------------

func (sb *svcBuilder) compose(file string) {
	data := sb.b.read(file)
	var doc yaml.Node
	if yaml.Unmarshal(data, &doc) != nil || len(doc.Content) == 0 {
		return
	}
	services := mapGet(doc.Content[0], "services")
	if services == nil || services.Kind != yaml.MappingNode {
		return
	}
	deployed := sb.projectOfDir(path.Dir(file))
	for i := 0; i+1 < len(services.Content); i += 2 {
		key, svc := services.Content[i], services.Content[i+1]
		name := key.Value
		var def Service
		def.Via = []string{"docker-compose"}
		def.Envs = []string{"compose"}
		def.Sources = []Evidence{{Path: file, Line: key.Line}}
		def.Image = scalar(mapGet(svc, "image"))
		if cn := scalar(mapGet(svc, "container_name")); cn != "" {
			def.aliases = []string{cn}
		}
		def.Command = commandText(mapGet(svc, "command"))
		for _, p := range seq(mapGet(svc, "ports")) {
			if n := containerPort(scalar(p)); n > 0 {
				def.Ports = append(def.Ports, n)
			}
		}
		for _, p := range seq(mapGet(svc, "expose")) {
			if n, err := strconv.Atoi(strings.TrimSpace(scalar(p))); err == nil {
				def.Ports = append(def.Ports, n)
			}
		}
		code, buildDir := "", ""
		if build := mapGet(svc, "build"); build != nil {
			ctx := scalar(build)
			if build.Kind == yaml.MappingNode {
				ctx = scalar(mapGet(build, "context"))
			}
			if ctx == "" {
				ctx = "."
			}
			buildDir = path.Clean(path.Join(path.Dir(file), ctx))
			code = sb.projectOfDir(buildDir)
		} else if backingKind(def.Image) == "" {
			code = sb.codeProject(def.Image, name, deployed)
		}
		def.Project = code
		if code != deployed {
			def.Deployed = deployed
		}
		def.Kind = sb.kindFor(def.Image, def.Ports, name, buildDir != "" && sb.isFrontendDir(buildDir))
		sb.add(name, def)
		for _, d := range dependsOn(mapGet(svc, "depends_on")) {
			sb.pending = append(sb.pending, pendingLink{name, d.Value, Evidence{Path: file, Line: d.Line, Text: strings.TrimSpace(d.Value)}})
		}
	}
}

func dependsOn(n *yaml.Node) []*yaml.Node {
	if n == nil {
		return nil
	}
	switch n.Kind {
	case yaml.SequenceNode:
		return n.Content
	case yaml.MappingNode:
		var out []*yaml.Node
		for i := 0; i < len(n.Content); i += 2 {
			out = append(out, n.Content[i])
		}
		return out
	}
	return nil
}

// containerPort reads "8080", "8080:80", "127.0.0.1:8080:80/tcp" → the container port.
func containerPort(s string) int {
	s = strings.TrimSpace(strings.Split(s, "/")[0])
	parts := strings.Split(s, ":")
	n, err := strconv.Atoi(parts[len(parts)-1])
	if err != nil {
		return 0
	}
	return n
}

// ---- Kubernetes ----------------------------------------------------------------------

var workloadKinds = map[string]string{"Deployment": "", "StatefulSet": "", "DaemonSet": "", "CronJob": KindCron, "Job": KindJob}

func (sb *svcBuilder) kubernetes(file string) {
	data := sb.b.read(file)
	deployed := sb.projectOfDir(path.Dir(file))
	dec := yaml.NewDecoder(bytes.NewReader(data))
	type svcSel struct {
		name string
		sel  map[string]string
		line int
	}
	var selectors []svcSel
	labelsOf := map[string]map[string]string{} // workload → pod template labels
	for {
		var doc yaml.Node
		if dec.Decode(&doc) != nil {
			break
		}
		if len(doc.Content) == 0 {
			continue
		}
		root := doc.Content[0]
		kind := scalar(mapGet(root, "kind"))
		meta := mapGet(root, "metadata")
		nameNode := mapGet(meta, "name")
		name := scalar(nameNode)
		if name == "" {
			continue
		}
		line := root.Line
		if nameNode != nil {
			line = nameNode.Line
		}
		if kind == "Service" {
			sel := map[string]string{}
			if s := mapGet(mapGet(root, "spec"), "selector"); s != nil {
				for i := 0; i+1 < len(s.Content); i += 2 {
					sel[s.Content[i].Value] = s.Content[i+1].Value
				}
			}
			selectors = append(selectors, svcSel{name, sel, line})
			continue
		}
		fixed, ok := workloadKinds[kind]
		if !ok {
			continue
		}
		pod := mapGet(mapGet(root, "spec"), "template")
		if kind == "CronJob" {
			pod = mapGet(mapGet(mapGet(mapGet(root, "spec"), "jobTemplate"), "spec"), "template")
		}
		labels := map[string]string{}
		if l := mapGet(mapGet(pod, "metadata"), "labels"); l != nil {
			for i := 0; i+1 < len(l.Content); i += 2 {
				labels[l.Content[i].Value] = l.Content[i+1].Value
			}
		}
		labelsOf[name] = labels
		var def Service
		def.Via = []string{"kubernetes"}
		if e := envOf(file); e != "" {
			def.Envs = []string{e}
		}
		def.Sources = []Evidence{{Path: file, Line: line, Text: kind + " " + name}}
		for _, c := range seq(mapGet(mapGet(pod, "spec"), "containers")) {
			if def.Image == "" {
				def.Image = scalar(mapGet(c, "image"))
			}
			if def.Command == "" {
				def.Command = strings.TrimSpace(commandText(mapGet(c, "command")) + " " + commandText(mapGet(c, "args")))
			}
			for _, p := range seq(mapGet(c, "ports")) {
				if n, err := strconv.Atoi(scalar(mapGet(p, "containerPort"))); err == nil {
					def.Ports = append(def.Ports, n)
				}
			}
		}
		def.Project = sb.codeProject(def.Image, name, deployed)
		if bk := backingKind(def.Image); bk != "" {
			def.Project = ""
		}
		if def.Project != deployed {
			def.Deployed = deployed
		}
		def.Kind = fixed
		if def.Kind == "" {
			def.Kind = sb.kindFor(def.Image, def.Ports, name, false)
		}
		sb.add(name, def)
	}
	// A k8s Service is how others reach a workload: its name becomes an alias.
	for _, s := range selectors {
		for wl, labels := range labelsOf {
			if len(s.sel) == 0 {
				continue
			}
			match := true
			for k, v := range s.sel {
				if labels[k] != v {
					match = false
					break
				}
			}
			if match {
				if svc := sb.byName[wl]; svc != nil && s.name != wl {
					svc.aliases = union(svc.aliases, []string{s.name})
				}
			}
		}
	}
}

// ---- Dockerfiles -----------------------------------------------------------------------

var (
	exposeRe = regexp.MustCompile(`(?im)^\s*EXPOSE\s+(.+)$`)
	cmdRe    = regexp.MustCompile(`(?im)^\s*(CMD|ENTRYPOINT)\s+(.+)$`)
)

// dockerfiles adds an image service for a project with a Dockerfile and nothing that
// deploys it (no compose service or workload runs its code).
func (sb *svcBuilder) dockerfiles(snap *store.Snapshot) {
	covered := map[string]bool{}
	for _, s := range sb.byName {
		covered[s.Project] = true
	}
	for _, p := range snap.Manifests["Dockerfile"] {
		proj := sb.projectOfDir(path.Dir(p))
		if covered[proj] {
			continue
		}
		data := sb.b.read(p)
		var def Service
		def.Via = []string{"dockerfile"}
		def.Sources = []Evidence{{Path: p, Line: 1}}
		def.Project = proj
		for _, m := range exposeRe.FindAllSubmatch(data, -1) {
			for _, f := range strings.Fields(string(m[1])) {
				if n := containerPort(f); n > 0 {
					def.Ports = append(def.Ports, n)
				}
			}
		}
		if m := cmdRe.FindAllSubmatch(data, -1); len(m) > 0 {
			def.Command = strings.Trim(string(m[len(m)-1][2]), " []")
		}
		def.Kind = sb.kindFor("", def.Ports, path.Base(path.Dir(p)), sb.isFrontendDir(path.Dir(p)))
		name := path.Base(path.Dir(p))
		if proj != "" {
			name = sb.b.projects[sb.byProject(proj)].Name
			if d := path.Dir(p); d != proj {
				name += "-" + path.Base(d)
			}
		}
		if base := path.Base(p); strings.HasPrefix(base, "Dockerfile.") {
			name += "-" + strings.TrimPrefix(base, "Dockerfile.")
		}
		if name == "." || name == "" {
			name = "app"
		}
		sb.add(name, def)
	}
}

// ---- calls between services -----------------------------------------------------------

// A URL in a service's configuration whose host names another service.
var urlHostRe = regexp.MustCompile(`(?i)\b(?:https?|grpc|amqps?|redis|rediss|postgres(?:ql)?|mysql|mongodb(?:\+srv)?|nats|kafka|ws|wss)://(?:[^@/\s"']+@)?([a-z0-9][a-z0-9-]*)(?:\.[a-z0-9.-]+)?(?::\d+)?`)

func (sb *svcBuilder) calls(compose, kube map[string]bool) {
	names := map[string]string{} // reachable name → service
	for n, s := range sb.byName {
		names[strings.ToLower(n)] = n
		for _, a := range s.aliases {
			names[strings.ToLower(a)] = n
		}
	}
	// Which service a config line belongs to: compose service block or k8s doc name.
	scan := func(file string, owner func(line int) string) {
		data := sb.b.read(file)
		for i, line := range strings.Split(string(data), "\n") {
			if isComment(line) {
				continue
			}
			from := owner(i + 1)
			if from == "" {
				continue
			}
			for _, m := range urlHostRe.FindAllStringSubmatch(line, -1) {
				if to, ok := names[strings.ToLower(m[1])]; ok && to != from {
					sb.link(from, to, "calls", Evidence{Path: file, Line: i + 1, Text: strings.TrimSpace(line)})
				}
			}
		}
	}
	for f := range compose {
		spans := sb.composeSpans(f)
		scan(f, func(line int) string {
			for _, sp := range spans {
				if line >= sp.from && line <= sp.to {
					return sp.name
				}
			}
			return ""
		})
	}
	for f := range kube {
		// A ConfigMap or workload belongs to the service named like it (nexus-backend-config
		// → nexus-backend-app) — the longest service name it starts with or contains.
		data := sb.b.read(f)
		var spans []span
		dec := yaml.NewDecoder(bytes.NewReader(data))
		last := 0
		for {
			var doc yaml.Node
			if dec.Decode(&doc) != nil {
				break
			}
			if len(doc.Content) == 0 {
				continue
			}
			name := strings.ToLower(scalar(mapGet(mapGet(doc.Content[0], "metadata"), "name")))
			owner := ""
			for n, s := range names {
				if strings.Contains(name, strings.TrimSuffix(strings.TrimSuffix(n, "-app"), "-service")) && len(n) > len(owner) {
					owner = s
				}
			}
			spans = append(spans, span{owner, last + 1, 1 << 30})
			if len(spans) > 1 {
				spans[len(spans)-2].to = doc.Content[0].Line - 1
			}
			last = doc.Content[0].Line
		}
		scan(f, func(line int) string {
			for i := len(spans) - 1; i >= 0; i-- {
				if line >= spans[i].from {
					return spans[i].name
				}
			}
			return ""
		})
	}
}

type span struct {
	name     string
	from, to int
}

func (sb *svcBuilder) composeSpans(file string) []span {
	var doc yaml.Node
	if yaml.Unmarshal(sb.b.read(file), &doc) != nil || len(doc.Content) == 0 {
		return nil
	}
	services := mapGet(doc.Content[0], "services")
	if services == nil {
		return nil
	}
	var out []span
	for i := 0; i+1 < len(services.Content); i += 2 {
		sp := span{name: services.Content[i].Value, from: services.Content[i].Line, to: 1 << 30}
		if i+2 < len(services.Content) {
			sp.to = services.Content[i+2].Line - 1
		}
		out = append(out, sp)
	}
	return out
}

func (sb *svcBuilder) link(from, to, kind string, ev Evidence) {
	if sb.byName[to] == nil || from == to {
		return
	}
	k := [3]string{from, to, kind}
	l := sb.links[k]
	if l == nil {
		l = &ServiceLink{From: from, To: to, Kind: kind}
		sb.links[k] = l
	}
	if len(l.Evidence) < 6 {
		l.Evidence = append(l.Evidence, ev)
	}
}

// ---- YAML helpers ---------------------------------------------------------------------

func mapGet(n *yaml.Node, key string) *yaml.Node {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	return nil
}

func scalar(n *yaml.Node) string {
	if n == nil || n.Kind != yaml.ScalarNode {
		return ""
	}
	return n.Value
}

func seq(n *yaml.Node) []*yaml.Node {
	if n == nil || n.Kind != yaml.SequenceNode {
		return nil
	}
	return n.Content
}

func commandText(n *yaml.Node) string {
	if n == nil {
		return ""
	}
	if n.Kind == yaml.ScalarNode {
		return n.Value
	}
	var parts []string
	for _, c := range seq(n) {
		parts = append(parts, scalar(c))
	}
	return strings.Join(parts, " ")
}

// roles labels each project by what it is for the system.
func (b *builder) roles(services []Service) {
	runs := map[string][]string{}
	for _, s := range services {
		if s.Project != "" {
			runs[s.Project] = union(runs[s.Project], []string{s.Kind})
		}
	}
	usedAsLib := map[string]bool{}
	for _, l := range b.links {
		if l.Kind == Imports || l.Kind == DependsOn {
			usedAsLib[l.To] = true
		}
	}
	for i := range b.projects {
		p := &b.projects[i]
		var r []string
		kinds := runs[p.Path]
		if b.isInfra(i) {
			p.Roles = []string{"infrastructure"} // mostly deployment files, whatever it runs
			continue
		}
		has := func(k string) bool { return contains(kinds, k) }
		if has(KindFrontend) {
			r = append(r, "frontend")
		}
		if has(KindAPI) || has(KindWorker) || has(KindCron) || has(KindJob) {
			r = append(r, "service")
		}
		if usedAsLib[p.Path] && len(r) == 0 {
			r = append(r, "library")
		}
		if len(r) == 0 && b.isInfra(i) {
			r = append(r, "infrastructure")
		}
		if len(r) == 0 && b.served[i] {
			r = append(r, "service")
		}
		p.Roles = r
		if p.Roles == nil {
			p.Roles = []string{}
		}
	}
}

// isInfra: mostly deployment files (YAML, Terraform, Helm) rather than application code.
func (b *builder) isInfra(i int) bool {
	p := b.projects[i]
	code := 0
	for _, l := range p.Langs {
		code += l.Lines
	}
	return b.configLines[i] > code
}

type pendingLink struct {
	from, to string
	ev       Evidence
}
