// Package ops lists what can be done on the box, once, for the JSON API and
// the MCP server. Each module of the app adds its operations with Register.
package ops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// Op is one call to the Freebox OS API.
type Op struct {
	Name        string // snake_case, unique: "lan_hosts_list"
	Description string // for people and for language models: what, and the key fields
	Method      string // GET, POST, PUT, DELETE
	// Path is relative to /api/v16/. {name} segments are filled from the
	// argument of the same name, escaped: "dhcp/static_lease/{id}".
	Path   string
	Params []Param
	// Body, when true, sends the "body" argument as the JSON request body.
	Body     bool
	BodyHelp string // describes the body object's fields
	// Dangerous operations can cut the network or lose data: they run only
	// with "confirm": true in the arguments.
	Dangerous bool
}

// Param is a path or query parameter.
type Param struct {
	Name        string
	Type        string // "string", "integer", "boolean"
	Description string
	Required    bool
	Query       bool // sent as ?name=value instead of in the path
}

// Box is what an operation needs from the API client.
type Box interface {
	Do(ctx context.Context, method, path string, in, out any) error
}

var (
	mu  sync.Mutex
	all = map[string]Op{}
)

var namePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// Register adds operations; it panics on a duplicate or malformed one, which
// only a programming error can cause.
func Register(list ...Op) {
	mu.Lock()
	defer mu.Unlock()
	for _, op := range list {
		if !namePattern.MatchString(op.Name) || op.Description == "" || op.Path == "" {
			panic("ops: malformed operation " + op.Name)
		}
		if _, dup := all[op.Name]; dup {
			panic("ops: duplicate operation " + op.Name)
		}
		if op.Method == "" {
			op.Method = http.MethodGet
		}
		all[op.Name] = op
	}
}

// All returns every operation, sorted by name.
func All() []Op {
	mu.Lock()
	defer mu.Unlock()
	list := make([]Op, 0, len(all))
	for _, op := range all {
		list = append(list, op)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	return list
}

// Get returns the operation with that name.
func Get(name string) (Op, bool) {
	mu.Lock()
	defer mu.Unlock()
	op, ok := all[name]
	return op, ok
}

// ErrConfirm is returned when a dangerous operation is called without
// "confirm": true.
var ErrConfirm = errors.New(`this action can cut the network or lose data: ask the user, then call again with "confirm": true`)

// Call runs op with the given arguments and returns the box's result.
func Call(ctx context.Context, box Box, op Op, args map[string]any) (json.RawMessage, error) {
	if op.Dangerous {
		if c, _ := args["confirm"].(bool); !c {
			return nil, ErrConfirm
		}
	}
	path := op.Path
	q := url.Values{}
	for _, p := range op.Params {
		v, ok := args[p.Name]
		if !ok || v == nil {
			if p.Required {
				return nil, fmt.Errorf("missing argument %q", p.Name)
			}
			if !p.Query {
				return nil, fmt.Errorf("missing path argument %q", p.Name)
			}
			continue
		}
		s, err := scalar(v)
		if err != nil {
			return nil, fmt.Errorf("argument %q: %w", p.Name, err)
		}
		if p.Query {
			q.Set(p.Name, s)
		} else {
			path = strings.ReplaceAll(path, "{"+p.Name+"}", url.PathEscape(s))
		}
	}
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	var body any
	if op.Body {
		b, ok := args["body"]
		if !ok {
			return nil, errors.New(`missing argument "body"`)
		}
		body = b
	}
	var out json.RawMessage
	if err := box.Do(ctx, op.Method, path, body, &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = json.RawMessage(`{"success":true}`)
	}
	return out, nil
}

func scalar(v any) (string, error) {
	switch x := v.(type) {
	case string:
		return x, nil
	case bool:
		return fmt.Sprint(x), nil
	case float64:
		if x != float64(int64(x)) {
			return "", errors.New("must be an integer")
		}
		return fmt.Sprint(int64(x)), nil
	case json.Number:
		return x.String(), nil
	}
	return "", fmt.Errorf("unsupported type %T", v)
}

// Schema returns the JSON Schema of op's arguments.
func (op Op) Schema() map[string]any {
	props := map[string]any{}
	var required []string
	for _, p := range op.Params {
		t := p.Type
		if t == "" {
			t = "string"
		}
		props[p.Name] = map[string]any{"type": t, "description": p.Description}
		if p.Required || !p.Query {
			required = append(required, p.Name)
		}
	}
	if op.Body {
		props["body"] = map[string]any{"type": "object", "description": op.BodyHelp}
		required = append(required, "body")
	}
	if op.Dangerous {
		props["confirm"] = map[string]any{"type": "boolean", "description": "Must be true, after the user agreed: this action can cut the network or lose data."}
		required = append(required, "confirm")
	}
	s := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}
