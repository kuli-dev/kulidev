package scan

import "strings"

// Code generated from Python 3.12.12 sys.stdlib_module_names and the Node.js
// builtin module list. Standard libraries are excluded from "external dependencies".

var pythonStdlib = map[string]bool{
	"abc": true, "aifc": true, "antigravity": true, "argparse": true, "array": true, "ast": true,
	"asyncio": true, "atexit": true, "audioop": true, "base64": true, "bdb": true, "binascii": true,
	"bisect": true, "builtins": true, "bz2": true, "cProfile": true, "calendar": true, "cgi": true,
	"cgitb": true, "chunk": true, "cmath": true, "cmd": true, "code": true, "codecs": true,
	"codeop": true, "collections": true, "colorsys": true, "compileall": true, "concurrent": true,
	"configparser": true, "contextlib": true, "contextvars": true, "copy": true, "copyreg": true,
	"crypt": true, "csv": true, "ctypes": true, "curses": true, "dataclasses": true, "datetime": true,
	"dbm": true, "decimal": true, "difflib": true, "dis": true, "doctest": true, "email": true,
	"encodings": true, "ensurepip": true, "enum": true, "errno": true, "faulthandler": true,
	"fcntl": true, "filecmp": true, "fileinput": true, "fnmatch": true, "fractions": true,
	"ftplib": true, "functools": true, "gc": true, "genericpath": true, "getopt": true,
	"getpass": true, "gettext": true, "glob": true, "graphlib": true, "grp": true, "gzip": true,
	"hashlib": true, "heapq": true, "hmac": true, "html": true, "http": true, "idlelib": true,
	"imaplib": true, "imghdr": true, "importlib": true, "inspect": true, "io": true,
	"ipaddress": true, "itertools": true, "json": true, "keyword": true, "lib2to3": true,
	"linecache": true, "locale": true, "logging": true, "lzma": true, "mailbox": true,
	"mailcap": true, "marshal": true, "math": true, "mimetypes": true, "mmap": true,
	"modulefinder": true, "msilib": true, "msvcrt": true, "multiprocessing": true, "netrc": true,
	"nis": true, "nntplib": true, "nt": true, "ntpath": true, "nturl2path": true, "numbers": true,
	"opcode": true, "operator": true, "optparse": true, "os": true, "ossaudiodev": true,
	"pathlib": true, "pdb": true, "pickle": true, "pickletools": true, "pipes": true, "pkgutil": true,
	"platform": true, "plistlib": true, "poplib": true, "posix": true, "posixpath": true,
	"pprint": true, "profile": true, "pstats": true, "pty": true, "pwd": true, "py_compile": true,
	"pyclbr": true, "pydoc": true, "pydoc_data": true, "pyexpat": true, "queue": true, "quopri": true,
	"random": true, "re": true, "readline": true, "reprlib": true, "resource": true,
	"rlcompleter": true, "runpy": true, "sched": true, "secrets": true, "select": true,
	"selectors": true, "shelve": true, "shlex": true, "shutil": true, "signal": true, "site": true,
	"smtplib": true, "sndhdr": true, "socket": true, "socketserver": true, "spwd": true,
	"sqlite3": true, "sre_compile": true, "sre_constants": true, "sre_parse": true, "ssl": true,
	"stat": true, "statistics": true, "string": true, "stringprep": true, "struct": true,
	"subprocess": true, "sunau": true, "symtable": true, "sys": true, "sysconfig": true,
	"syslog": true, "tabnanny": true, "tarfile": true, "telnetlib": true, "tempfile": true,
	"termios": true, "textwrap": true, "this": true, "threading": true, "time": true, "timeit": true,
	"tkinter": true, "token": true, "tokenize": true, "tomllib": true, "trace": true,
	"traceback": true, "tracemalloc": true, "tty": true, "turtle": true, "turtledemo": true,
	"types": true, "typing": true, "unicodedata": true, "unittest": true, "urllib": true, "uu": true,
	"uuid": true, "venv": true, "warnings": true, "wave": true, "weakref": true, "webbrowser": true,
	"winreg": true, "winsound": true, "wsgiref": true, "xdrlib": true, "xml": true, "xmlrpc": true,
	"zipapp": true, "zipfile": true, "zipimport": true, "zlib": true, "zoneinfo": true,
}

var nodeBuiltins = map[string]bool{
	"assert": true, "async_hooks": true, "buffer": true, "child_process": true, "cluster": true,
	"console": true, "constants": true, "crypto": true, "dgram": true, "diagnostics_channel": true,
	"dns": true, "domain": true, "events": true, "fs": true, "http": true, "http2": true,
	"https": true, "inspector": true, "module": true, "net": true, "os": true, "path": true,
	"perf_hooks": true, "process": true, "punycode": true, "querystring": true, "readline": true,
	"repl": true, "stream": true, "string_decoder": true, "sys": true, "timers": true, "tls": true,
	"trace_events": true, "tty": true, "url": true, "util": true, "v8": true, "vm": true,
	"wasi": true, "worker_threads": true, "zlib": true,
}

// IsStdlib reports whether an external module name ("go:fmt", "py:os", "npm:fs",
// "node:fs") belongs to its language's standard library.
func IsStdlib(name string) bool {
	eco, mod, ok := strings.Cut(name, ":")
	if !ok {
		return false
	}
	switch eco {
	case "go":
		first, _, _ := strings.Cut(mod, "/")
		return !strings.Contains(first, ".")
	case "py":
		return pythonStdlib[mod]
	case "node":
		return true
	case "java":
		return strings.HasPrefix(mod, "java.") || strings.HasPrefix(mod, "javax.") || strings.HasPrefix(mod, "jdk.")
	case "php":
		return !strings.Contains(mod, "\\") && phpGlobal(mod) // use Exception; use DateTime;
	case "npm":
		base, _, _ := strings.Cut(strings.TrimPrefix(mod, "node:"), "/")
		return strings.HasPrefix(mod, "node:") || nodeBuiltins[base]
	}
	return false
}

// phpGlobal reports whether a single-segment PHP name is a built-in class.
func phpGlobal(name string) bool {
	switch name {
	case "Exception", "Throwable", "Error", "DateTime", "DateTimeImmutable", "DateTimeInterface", "DateInterval",
		"Closure", "Generator", "ArrayObject", "ArrayIterator", "Iterator", "IteratorAggregate", "Countable",
		"JsonSerializable", "Stringable", "Traversable", "PDO", "PDOException", "stdClass", "InvalidArgumentException",
		"RuntimeException", "LogicException", "TypeError", "ValueError", "SplObjectStorage":
		return true
	}
	return false
}
