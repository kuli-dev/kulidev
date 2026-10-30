package systems

import (
	"bufio"
	"bytes"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/kuli-dev/kulidev/internal/store"
)

// CallsAPI: code in From calls an HTTP route that To serves.
const CallsAPI = "calls API"

// Route definitions, by framework. The last capture group is the path.
var routeRes = []*regexp.Regexp{
	// FastAPI / Flask / Starlette: @router.get("/orders/{id}"), @app.route("/x")
	regexp.MustCompile(`@\w+\.(?:get|post|put|patch|delete|route|api_route|websocket)\(\s*["']([^"']*)["']`),
	// Express / Koa / Fastify: router.get('/orders/:id', …)
	regexp.MustCompile(`\b(?:app|router|server|api|fastify)\.(?:get|post|put|patch|delete|all)\(\s*['"` + "`" + `]([^'"` + "`" + `]+)['"` + "`" + `]`),
	// NestJS: @Get('orders/:id')
	regexp.MustCompile(`@(?:Get|Post|Put|Patch|Delete|All)\(\s*['"]([^'"]*)['"]`),
	// Go: http.HandleFunc("/x"), mux.HandleFunc("GET /x"), gin/echo r.GET("/x")
	regexp.MustCompile(`\.(?:HandleFunc|Handle)\(\s*"(?:[A-Z]+ )?(/[^"]*)"`),
	regexp.MustCompile(`\.(?:GET|POST|PUT|PATCH|DELETE)\(\s*"(/[^"]*)"`),
	// Spring: @GetMapping("/x"), @RequestMapping(value = "/x")
	regexp.MustCompile(`@(?:Get|Post|Put|Patch|Delete|Request)Mapping\(\s*(?:value\s*=\s*|path\s*=\s*)?\{?\s*"([^"]*)"`),
	// Laravel: Route::get('/x', …)
	regexp.MustCompile(`Route::(?:get|post|put|patch|delete|any|match)\(\s*['"]([^'"]+)['"]`),
}

// Prefixes that apply to every route in the file.
var prefixRes = []*regexp.Regexp{
	regexp.MustCompile(`APIRouter\([^)]*prefix\s*=\s*["']([^"']+)["']`),
	regexp.MustCompile(`Blueprint\([^)]*url_prefix\s*=\s*["']([^"']+)["']`),
	regexp.MustCompile(`@Controller\(\s*['"]([^'"]+)['"]`),
	regexp.MustCompile(`@RequestMapping\(\s*(?:value\s*=\s*|path\s*=\s*)?"([^"]+)"\s*\)\s*$`),
}

// Calls: a path-looking string on a line that makes an HTTP request or builds a URL.
var (
	callLineRe = regexp.MustCompile(`(?i)fetch\(|axios|\$http|\bhttp\.|requests\.|httpx|aiohttp|client\.|\bapi\.|\.(?:get|post|put|patch|delete|request)\(|\burl\s*[:=]|url\(|_url\s*[:=]|endpoint\s*[:=]`)
	// Hosts a call may name and still be one of these services: local, an IP, a cluster
	// service name, or a variable.
	externalHostRe = regexp.MustCompile(`^https?://([a-z0-9-]+\.)+[a-z]{2,}(?::\d+)?/`)
	callStrRe      = regexp.MustCompile("[\"'`]((?:https?://[^/\"'`\\s]+)?(?:\\$\\{[^}]+\\})?/[A-Za-z0-9_\\-./{}:$<>]*)[\"'`?]")
	fsPathRe       = regexp.MustCompile(`^/(?:tmp|usr|etc|var|dev|home|opt|proc|bin|sbin|mnt|srv|root|app|data|repos|workspace)(?:/|$)`)
	paramRe        = regexp.MustCompile(`^(?:\{[^}]*\}|:[A-Za-z_]\w*|<[^>]*>|\$\{[^}]*\}|\*)$`)
)

var webExt = map[string]bool{".py": true, ".js": true, ".jsx": true, ".ts": true, ".tsx": true, ".mjs": true,
	".go": true, ".java": true, ".kt": true, ".php": true, ".vue": true, ".svelte": true}

// Segments too generic to prove which service is called on their own.
var weakSeg = map[string]bool{"api": true, "v1": true, "v2": true, "v3": true, "health": true, "healthz": true,
	"status": true, "metrics": true, "login": true, "logout": true, "docs": true, "ping": true, "ready": true,
	"version": true, "static": true, "assets": true, "index": true, "auth": true, "users": true, "me": true}

type route struct {
	segs    []string
	project int
	ev      Evidence
}

// splitPath normalises a URL path to segments; parameters become "*".
func splitPath(p string) []string {
	if i := strings.Index(p, "://"); i >= 0 {
		rest := p[i+3:]
		if j := strings.IndexByte(rest, '/'); j >= 0 {
			p = rest[j:]
		} else {
			return nil
		}
	}
	if i := strings.IndexAny(p, "?#"); i >= 0 {
		p = p[:i]
	}
	// "${BASE}/orders": the base is unknown, keep what follows it.
	if strings.HasPrefix(p, "${") {
		if i := strings.IndexByte(p, '}'); i >= 0 {
			p = p[i+1:]
		}
	}
	var segs []string
	for _, s := range strings.Split(p, "/") {
		if s == "" || s == "." {
			continue
		}
		if paramRe.MatchString(s) || strings.Contains(s, "${") || strings.Contains(s, "{") {
			segs = append(segs, "*")
			continue
		}
		segs = append(segs, strings.ToLower(s))
	}
	return segs
}

func literal(segs []string) (n int, strong bool) {
	for _, s := range segs {
		if s != "*" {
			n++
			if !weakSeg[s] && len(s) >= 3 {
				strong = true
			}
		}
	}
	return n, strong
}

// matchScore aligns the shorter path with the tail of the longer one (a call through
// a base URL holding /api/v1, or a route mounted under a prefix not seen) and counts
// the literal segments both sides share. Parameters match anything but prove nothing,
// so a route made only of parameters never matches.
func matchScore(call, rt []string) int {
	a, b := call, rt
	if len(a) > len(b) {
		a, b = b, a
	}
	if len(a) == 0 {
		return 0
	}
	off := len(b) - len(a)
	score, strong := 0, false
	for i := range a {
		x, y := a[i], b[off+i]
		switch {
		case x == "*" || y == "*":
		case x == y:
			score++
			strong = strong || !weakSeg[x] && len(x) >= 3
		default:
			return 0
		}
	}
	if !strong || score < 2 && len(call) != len(rt) {
		return 0
	}
	return score
}

// http links a project to another whose HTTP routes its code calls.
func (b *builder) http(snap *store.Snapshot) {
	var routes []route
	type call struct {
		segs    []string
		project int
		ev      Evidence
	}
	var calls []call
	for _, f := range snap.Files {
		if !webExt[path.Ext(f.Path)] {
			continue
		}
		proj := b.of(f.Path)
		if proj < 0 {
			continue
		}
		data := b.read(f.Path)
		if len(data) == 0 || len(data) > 512<<10 {
			continue
		}
		var prefix []string
		for _, re := range prefixRes {
			if m := re.FindSubmatch(data); m != nil {
				prefix = splitPath(string(m[1]))
				break
			}
		}
		sc := bufio.NewScanner(bytes.NewReader(data))
		sc.Buffer(make([]byte, 64<<10), 1<<20)
		for n := 1; sc.Scan(); n++ {
			line := sc.Text()
			if isComment(line) {
				continue
			}
			defined := false
			for _, re := range routeRes {
				if !maybeRoute(line) {
					break
				}
				if m := re.FindStringSubmatch(line); m != nil {
					segs := append(append([]string{}, prefix...), splitPath(m[len(m)-1])...)
					if len(segs) > 0 {
						routes = append(routes, route{segs, proj, Evidence{Path: f.Path, Line: n, Text: strings.TrimSpace(line)}})
						b.served[proj] = true
					}
					defined = true
					break
				}
			}
			if defined || !strings.Contains(line, "/") || !strings.ContainsAny(line, "\"'`") || !callLineRe.MatchString(line) {
				continue
			}
			if t := strings.TrimSpace(line); strings.HasPrefix(t, ">>>") || strings.HasPrefix(t, "*") || strings.HasPrefix(t, "e.g.") {
				continue // docstring examples
			}
			for _, m := range callStrRe.FindAllStringSubmatch(line, 4) {
				u := m[1]
				if externalHostRe.MatchString(strings.ToLower(u)) && !strings.Contains(u, ".svc") && !strings.Contains(u, ".local") {
					continue // another company's API (googleapis.com, …)
				}
				if fsPathRe.MatchString(u) {
					continue
				}
				if segs := splitPath(u); len(segs) > 0 {
					calls = append(calls, call{segs, proj, Evidence{Path: f.Path, Line: n}})
				}
			}
		}
	}
	if len(routes) == 0 {
		return
	}
	for _, c := range calls {
		// The best-matching route wins; a tie between services is too ambiguous to draw,
		// and a project that serves the path itself is calling itself.
		best, tie := 0, false
		var hit route
		for _, r := range routes {
			sc := matchScore(c.segs, r.segs)
			switch {
			case sc > best:
				best, hit, tie = sc, r, false
			case sc == best && sc > 0 && r.project != hit.project:
				tie = true
			}
		}
		if best == 0 || tie || hit.project == c.project {
			continue
		}
		to := []int{hit.project}
		c.ev.Target = hit.ev.Path + ":" + itoa(hit.ev.Line) + "  " + hit.ev.Text
		b.add(c.project, to[0], CallsAPI, c.ev)
	}
}

func itoa(n int) string { return strconv.Itoa(n) }

// maybeRoute is a cheap check before the route regexes run on a line.
func maybeRoute(line string) bool {
	for _, k := range []string{"@", ".get(", ".post(", ".put(", ".patch(", ".delete(", ".all(", ".route(",
		"Handle", ".GET(", ".POST(", ".PUT(", ".PATCH(", ".DELETE(", "Route::"} {
		if strings.Contains(line, k) {
			return true
		}
	}
	return false
}
