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
	BodyHelp string // describes the body's fields
	// BodyArray marks a body that is a JSON array, not an object.
	BodyArray bool
	// Form sends the body object as application/x-www-form-urlencoded, for
	// the few endpoints that want it (downloads/add).
	Form bool
	// Read marks a POST that only reads (rrd/, batched file info): it is
	// announced read-only and allowed for read-only API tokens.
	Read bool
	// Secret marks a read that returns keys or passwords (Wi-Fi keys, VPN
	// configurations): read-only API tokens may not call it.
	Secret bool
	// Dangerous operations can cut the network or access, lose data, expose
	// the box or its secrets, or make it reach an arbitrary address: they run
	// only with "confirm": true in the arguments.
	Dangerous bool
}

// ReadOnly reports whether op changes nothing on the box.
func (op Op) ReadOnly() bool {
	return (op.Method == http.MethodGet || op.Read) && !op.Dangerous
}

// Param is a path or query parameter.
type Param struct {
	Name        string
	Type        string // "string", "integer", "boolean"
	Description string
	Required    bool
	Query       bool // sent as ?name=value instead of in the path
	// Base64 marks a path parameter holding a base64 value, such as a file
	// path for the fs/ API: it may contain "/" and is inserted as is.
	Base64 bool
}

var base64Value = regexp.MustCompile(`^[A-Za-z0-9+/]+={0,2}$`)

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
var ErrConfirm = errors.New(`this action can cut the network or access, lose data, or expose the box or its secrets: explain it to the user, then call again with "confirm": true once they agreed`)

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
		} else if p.Base64 {
			if !base64Value.MatchString(s) {
				return nil, fmt.Errorf("argument %q: not base64", p.Name)
			}
			path = strings.ReplaceAll(path, "{"+p.Name+"}", s)
		} else {
			if s == "" || s == "." || s == ".." || strings.ContainsAny(s, "/\\") {
				return nil, fmt.Errorf("argument %q: not a valid identifier", p.Name)
			}
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
		if op.Form {
			obj, ok := b.(map[string]any)
			if !ok {
				return nil, errors.New(`argument "body" must be an object`)
			}
			form := url.Values{}
			for k, v := range obj {
				sv, err := scalar(v)
				if err != nil {
					return nil, fmt.Errorf("body field %q: %w", k, err)
				}
				form.Set(k, sv)
			}
			body = form
		}
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
		t := "object"
		if op.BodyArray {
			t = "array"
		}
		props["body"] = map[string]any{"type": t, "description": op.BodyHelp}
		required = append(required, "body")
	}
	if op.Dangerous {
		props["confirm"] = map[string]any{"type": "boolean", "description": "Must be true, and only after the user agreed: this action can cut the network or access, lose data, or expose the box or its secrets."}
		required = append(required, "confirm")
	}
	s := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}
