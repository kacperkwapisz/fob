//go:build live

package app

import (
	"encoding/json"
	"path"
	"sort"
	"strconv"
	"strings"
)

// virtFS is an in-memory workspace that exists only for the live Cursor
// agentic run. The model never sees a real disk.
type virtFS struct {
	files map[string]string
	calls []string
}

const (
	workspaceSecret  = "amber-42"
	workspaceVersion = "1.4.0"
)

func newLanternWorkspace() *virtFS {
	return &virtFS{files: map[string]string{
		"/README.md": `# lantern

Internal test project. Release secret is in notes/secrets.env.
App version lives in src/app.ts.
`,
		"/src/app.ts": `export const name = "lantern"
export const version = "1.4.0"
`,
		"/src/util.ts": `export function banner() {
  return "lantern"
}
`,
		"/notes/todo.md": `- [ ] Ship the amber release
- [x] Bump version to 1.4.0
`,
		"/notes/secrets.env": "LANTERN_CODE=amber-42\n",
	}}
}

func workspacePrompt() string {
	return strings.TrimSpace(`
You are in a protocol test. A virtual workspace is mounted at /. It is not on the host disk.
Use the tools. Do not guess file contents — they are not in this prompt.

1. list_dir on /
2. Read whatever files you need (read_file or grep_files)
3. Find the release secret (LANTERN_CODE) and the app version
4. write_file /out/answer.txt with those two values, space-separated
5. Reply with only those two tokens
`)
}

func workspaceTools() []any {
	return []any{
		fnTool("list_dir", "List files and folders in the virtual workspace.", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{"type": "string", "description": "Directory path, e.g. / or /src"},
			},
			"required": []any{"path"},
		}),
		fnTool("read_file", "Read a file from the virtual workspace.", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{"type": "string", "description": "File path, e.g. /README.md"},
			},
			"required": []any{"path"},
		}),
		fnTool("grep_files", "Search file contents in the virtual workspace.", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{"type": "string", "description": "Substring to find"},
			},
			"required": []any{"query"},
		}),
		fnTool("write_file", "Write a file in the virtual workspace.", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":    map[string]any{"type": "string"},
				"content": map[string]any{"type": "string"},
			},
			"required": []any{"path", "content"},
		}),
	}
}

func fnTool(name, desc string, params map[string]any) map[string]any {
	return map[string]any{
		"type": "function",
		"function": map[string]any{
			"name": name, "description": desc, "parameters": params,
		},
	}
}

func (fs *virtFS) handle(name, argsJSON string) string {
	fs.calls = append(fs.calls, name)
	args := map[string]any{}
	_ = json.Unmarshal([]byte(argsJSON), &args)
	switch name {
	case "list_dir":
		return fs.list(asStr(args["path"]))
	case "read_file":
		return fs.read(asStr(args["path"]))
	case "grep_files":
		return fs.grep(asStr(args["query"]))
	case "write_file":
		return fs.write(asStr(args["path"]), asStr(args["content"]))
	default:
		return "unknown tool: " + name
	}
}

func virtPath(raw string) string {
	p := strings.TrimSpace(raw)
	if p == "" || p == "." {
		return "/"
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return path.Clean(p)
}

func (fs *virtFS) list(raw string) string {
	dir := virtPath(raw)
	prefix := dir
	if prefix != "/" {
		prefix += "/"
	}
	var names []string
	seen := map[string]bool{}
	for p := range fs.files {
		rel := ""
		if dir == "/" {
			rel = strings.TrimPrefix(p, "/")
		} else if strings.HasPrefix(p, prefix) {
			rel = strings.TrimPrefix(p, prefix)
		} else {
			continue
		}
		name, _, cut := strings.Cut(rel, "/")
		if name == "" {
			continue
		}
		if cut {
			name += "/"
		}
		if !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return "empty or not a directory: " + dir
	}
	sort.Strings(names)
	return strings.Join(names, "\n")
}

func (fs *virtFS) read(raw string) string {
	p := virtPath(raw)
	body, ok := fs.files[p]
	if !ok {
		return "not found: " + p
	}
	return body
}

func (fs *virtFS) grep(query string) string {
	query = strings.TrimSpace(query)
	if query == "" {
		return "query required"
	}
	var hits []string
	for p, body := range fs.files {
		for i, line := range strings.Split(body, "\n") {
			if strings.Contains(line, query) {
				hits = append(hits, p+":"+strconv.Itoa(i+1)+":"+line)
			}
		}
	}
	sort.Strings(hits)
	if len(hits) == 0 {
		return "no matches"
	}
	return strings.Join(hits, "\n")
}

func (fs *virtFS) write(raw, content string) string {
	p := virtPath(raw)
	if p == "/" {
		return "cannot write directory"
	}
	fs.files[p] = content
	return "wrote " + p
}

func (fs *virtFS) called(name string) bool {
	for _, c := range fs.calls {
		if c == name {
			return true
		}
	}
	return false
}
