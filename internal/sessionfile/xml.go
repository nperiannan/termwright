package sessionfile

import (
	"bytes"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

// The XML settings export format ("Export Settings" in the original
// application) nests <key name="..."> elements. Leaves are typed:
//
//	<string name="Hostname">host</string>
//	<dword name="[SSH2] Port">22</dword>         (decimal)
//	<binary name="X">0a0b0c</binary>             (hex)
//	<array name="Description"><string>a</string></array>
//
// A <key> that directly contains typed values is a session; a <key> that only
// contains other keys is a folder.

type xmlNode struct {
	XMLName  xml.Name
	Name     string    `xml:"name,attr"`
	Text     string    `xml:",chardata"`
	Children []xmlNode `xml:",any"`
}

// XMLSessions maps "Folder/Sub/Session" to its parsed settings.
type XMLSessions map[string]*File

// ParseXML extracts all sessions found under the top-level "Sessions" key.
// When the document has no "Sessions" key, every session-like key is used.
func ParseXML(r io.Reader) (XMLSessions, error) {
	var root xmlNode
	dec := xml.NewDecoder(r)
	dec.CharsetReader = func(_ string, in io.Reader) (io.Reader, error) { return in, nil }
	if err := dec.Decode(&root); err != nil {
		return nil, fmt.Errorf("parse xml: %w", err)
	}
	out := XMLSessions{}
	start := findKey(&root, "Sessions")
	if start == nil {
		start = &root
	}
	walkXML(start, "", out)
	return out, nil
}

func findKey(n *xmlNode, name string) *xmlNode {
	for i := range n.Children {
		c := &n.Children[i]
		if c.XMLName.Local == "key" && strings.EqualFold(c.Name, name) {
			return c
		}
		if f := findKey(c, name); f != nil {
			return f
		}
	}
	return nil
}

func walkXML(n *xmlNode, prefix string, out XMLSessions) {
	hasValues := false
	for _, c := range n.Children {
		if c.XMLName.Local != "key" {
			hasValues = true
			break
		}
	}
	if hasValues && prefix != "" && !strings.HasSuffix(prefix, "__FolderData__") {
		f := NewFile()
		for _, c := range n.Children {
			switch c.XMLName.Local {
			case "string":
				f.Entries = append(f.Entries, &Entry{Type: 'S', Name: c.Name, Str: c.Text})
			case "dword":
				v, _ := strconv.ParseInt(strings.TrimSpace(c.Text), 10, 64)
				f.Entries = append(f.Entries, &Entry{Type: 'D', Name: c.Name, DWord: uint32(v)})
			case "binary":
				h := strings.Join(strings.Fields(c.Text), "")
				b, _ := hex.DecodeString(h)
				f.Entries = append(f.Entries, &Entry{Type: 'B', Name: c.Name, Bin: b})
			case "array":
				var items []string
				for _, it := range c.Children {
					items = append(items, it.Text)
				}
				if items == nil {
					items = []string{}
				}
				f.Entries = append(f.Entries, &Entry{Type: 'Z', Name: c.Name, List: items})
			}
		}
		out[prefix] = f
	}
	for i := range n.Children {
		c := &n.Children[i]
		if c.XMLName.Local != "key" {
			continue
		}
		p := c.Name
		if prefix != "" {
			p = prefix + "/" + c.Name
		}
		walkXML(c, p, out)
	}
}

// WriteXML writes sessions in the XML settings format.
func WriteXML(w io.Writer, sessions XMLSessions) error {
	type tree struct {
		folders  map[string]*tree
		sessions map[string]*File
	}
	newTree := func() *tree { return &tree{folders: map[string]*tree{}, sessions: map[string]*File{}} }
	root := newTree()
	for path, f := range sessions {
		parts := strings.Split(path, "/")
		t := root
		for _, p := range parts[:len(parts)-1] {
			if t.folders[p] == nil {
				t.folders[p] = newTree()
			}
			t = t.folders[p]
		}
		t.sessions[parts[len(parts)-1]] = f
	}
	var b bytes.Buffer
	b.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<VanDyke version=\"3.0\">\n\t<key name=\"Sessions\">\n")
	esc := func(s string) string {
		var x bytes.Buffer
		_ = xml.EscapeText(&x, []byte(s))
		return x.String()
	}
	var emit func(t *tree, depth int)
	emit = func(t *tree, depth int) {
		ind := strings.Repeat("\t", depth)
		names := make([]string, 0, len(t.folders))
		for n := range t.folders {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			fmt.Fprintf(&b, "%s<key name=\"%s\">\n", ind, esc(n))
			emit(t.folders[n], depth+1)
			fmt.Fprintf(&b, "%s</key>\n", ind)
		}
		names = names[:0]
		for n := range t.sessions {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			fmt.Fprintf(&b, "%s<key name=\"%s\">\n", ind, esc(n))
			for _, e := range t.sessions[n].Entries {
				switch e.Type {
				case 'S':
					fmt.Fprintf(&b, "%s\t<string name=\"%s\">%s</string>\n", ind, esc(e.Name), esc(e.Str))
				case 'D':
					fmt.Fprintf(&b, "%s\t<dword name=\"%s\">%d</dword>\n", ind, esc(e.Name), e.DWord)
				case 'B':
					fmt.Fprintf(&b, "%s\t<binary name=\"%s\">%s</binary>\n", ind, esc(e.Name), hex.EncodeToString(e.Bin))
				case 'Z':
					fmt.Fprintf(&b, "%s\t<array name=\"%s\">\n", ind, esc(e.Name))
					for _, s := range e.List {
						fmt.Fprintf(&b, "%s\t\t<string>%s</string>\n", ind, esc(s))
					}
					fmt.Fprintf(&b, "%s\t</array>\n", ind)
				}
			}
			fmt.Fprintf(&b, "%s</key>\n", ind)
		}
	}
	emit(root, 2)
	b.WriteString("\t</key>\n</VanDyke>\n")
	_, err := w.Write(b.Bytes())
	return err
}
