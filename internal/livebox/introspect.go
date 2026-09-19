package livebox

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// ModelNode is a node from GET /sysbus/<path>?_restDepth=N datamodel introspection.
type ModelNode struct {
	ObjectInfo struct {
		Key     string `json:"key"`
		KeyPath string `json:"keyPath"`
		Name    string `json:"name"`
		Type    string `json:"type"`
	} `json:"objectInfo"`
	Functions []ModelFunction `json:"functions"`
	Children  []ModelNode     `json:"children"`
	Errors    []apiError      `json:"errors"`
}

// ModelFunction describes one sysbus method on a datamodel object.
type ModelFunction struct {
	Name       string          `json:"name"`
	Parameters []ModelParam    `json:"parameters"`
	Raw        json.RawMessage `json:"-"`
}

// ModelParam is a parameter definition from the datamodel.
type ModelParam struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// GetModel fetches the router datamodel for path (e.g. "Hosts", "NMC.Wifi", "Devices").
// depth -1 means full tree (same as rene-d sysbus -model).
func (c *Client) GetModel(ctx context.Context, path string, depth int) ([]ModelNode, error) {
	if c.ctxID == "" {
		return nil, fmt.Errorf("not logged in")
	}
	if path == "" {
		path = "sysbus"
	}
	// Normalize: Hosts -> sysbus/Hosts
	sysPath := "sysbus/" + path
	if len(path) >= 7 && path[:7] == "sysbus/" {
		sysPath = path
	} else if len(path) >= 7 && path[:7] == "sysbus." {
		sysPath = "sysbus/" + path[7:]
	}

	u := c.baseURL + "/" + sysPath + fmt.Sprintf("?_restDepth=%d", depth)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "text/javascript")
	req.Header.Set("X-Context", c.ctxID)
	req.Header.Set("Authorization", "X-Sah "+c.ctxID)
	req.Header.Set("X-Prototype-Version", "1.7")
	c.setBrowserHeaders(req)

	c.logf("→ GET %s\n", u)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	c.captureSetCookies(resp)

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	c.logf("← GET %s HTTP %d (%d bytes)\n", sysPath, resp.StatusCode, len(raw))

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s HTTP %d: %s", sysPath, resp.StatusCode, truncate(string(raw), 300))
	}

	// Firmware may return concatenated JSON objects `{}{}` — wrap as array.
	text := string(raw)
	if len(text) > 2 && text[0] == '{' {
		// Single object
		var one ModelNode
		if err := json.Unmarshal(raw, &one); err == nil {
			return []ModelNode{one}, nil
		}
		// Multiple concatenated objects
		fixed := "[" + replaceConcatJSON(text) + "]"
		var nodes []ModelNode
		if err := json.Unmarshal([]byte(fixed), &nodes); err != nil {
			return nil, fmt.Errorf("decode model %s: %w (head=%s)", sysPath, err, truncate(text, 200))
		}
		return nodes, nil
	}

	var nodes []ModelNode
	if err := json.Unmarshal(raw, &nodes); err != nil {
		return nil, fmt.Errorf("decode model %s: %w", sysPath, err)
	}
	return nodes, nil
}

func replaceConcatJSON(s string) string {
	out := make([]byte, 0, len(s)+16)
	for i := 0; i < len(s); i++ {
		out = append(out, s[i])
		if s[i] == '}' && i+1 < len(s) && s[i+1] == '{' {
			out = append(out, ',')
		}
	}
	return string(out)
}

// WalkFunctions calls fn for every function on every node in the model tree.
func WalkFunctions(nodes []ModelNode, fn func(path, method string, params []ModelParam)) {
	var walk func(prefix string, n ModelNode)
	walk = func(prefix string, n ModelNode) {
		name := n.ObjectInfo.Name
		if name == "" {
			name = n.ObjectInfo.Key
		}
		path := prefix
		if name != "" {
			if path == "" {
				path = name
			} else {
				path = path + "." + name
			}
		}
		for _, f := range n.Functions {
			fn(path, f.Name, f.Parameters)
		}
		for _, ch := range n.Children {
			walk(path, ch)
		}
	}
	for _, n := range nodes {
		walk("", n)
	}
}

// FindFunctions returns methods whose path or name matches filter (case-insensitive substring).
func FindFunctions(nodes []ModelNode, filter string) []struct {
	Path   string
	Method string
	Params []ModelParam
} {
	filter = strings.ToLower(filter)
	var out []struct {
		Path   string
		Method string
		Params []ModelParam
	}
	WalkFunctions(nodes, func(path, method string, params []ModelParam) {
		full := path + "." + method
		low := strings.ToLower(full)
		if filter == "" ||
			strings.Contains(low, filter) ||
			strings.Contains(strings.ToLower(path), filter) ||
			strings.Contains(strings.ToLower(method), filter) {
			out = append(out, struct {
				Path   string
				Method string
				Params []ModelParam
			}{path, method, params})
		}
	})
	return out
}
