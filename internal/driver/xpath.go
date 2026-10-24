package driver

import (
	"bytes"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/antchfx/htmlquery"
	"github.com/antchfx/xmlquery"
	"github.com/antchfx/xpath"
	"golang.org/x/net/html"
)

// The xpath selector of the one extraction primitive (#902, decision 1 of
// #1113): XPath 1.0 over an xml response (namespace-aware, the prefixes
// declared on the request) or over an html response parsed as a browser
// would (an OEM component's status page). A selector yields exactly one
// value: a scalar expression (count(), string()) its result, a node set of
// one node that node's text (an element's, trimmed) or value (an
// attribute's); none or many is a fault naming the count, never a silent
// first.

// compiledXPaths caches every compiled expression by its text and its
// namespace bindings, as compiled regexes are.
var compiledXPaths sync.Map // key -> *xpath.Expr

// xpathKey is an expression's cache key: its text and its sorted bindings.
func xpathKey(expr string, ns map[string]string) string {
	keys := make([]string, 0, len(ns))
	for k := range ns {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString(expr)
	for _, k := range keys {
		b.WriteString("\x00" + k + "=" + ns[k])
	}
	return b.String()
}

// compiledXPath compiles an expression under its namespace bindings once
// per process.
func compiledXPath(expr string, ns map[string]string) (*xpath.Expr, error) {
	key := xpathKey(expr, ns)
	if e, ok := compiledXPaths.Load(key); ok {
		return e.(*xpath.Expr), nil
	}
	e, err := xpath.CompileWithNS(expr, ns)
	if err != nil {
		return nil, err
	}
	actual, _ := compiledXPaths.LoadOrStore(key, e)
	return actual.(*xpath.Expr), nil
}

// parsedDocs holds a payload's documents, each parsed at most once however
// many emits read it.
type parsedDocs struct {
	xml     *xmlquery.Node
	xmlErr  error
	xmlDone bool
	htmlDoc *html.Node
	htmlErr error
	htmlOK  bool
}

// navigator is a fresh navigator over the payload's document in its format.
func (p Payload) navigator() (xpath.NodeNavigator, error) {
	if len(p.Body) == 0 {
		return nil, fmt.Errorf("the response carries no body")
	}
	docs := p.docs
	if docs == nil {
		docs = &parsedDocs{}
	}
	switch p.Format {
	case FormatXML:
		if !docs.xmlDone {
			docs.xml, docs.xmlErr = xmlquery.Parse(bytes.NewReader(p.Body))
			docs.xmlDone = true
		}
		if docs.xmlErr != nil {
			return nil, fmt.Errorf("the response is not xml: %v", docs.xmlErr)
		}
		return xmlquery.CreateXPathNavigator(docs.xml), nil
	case FormatHTML:
		if !docs.htmlOK {
			docs.htmlDoc, docs.htmlErr = htmlquery.Parse(bytes.NewReader(p.Body))
			docs.htmlOK = true
		}
		if docs.htmlErr != nil {
			return nil, fmt.Errorf("the response is not html: %v", docs.htmlErr)
		}
		return htmlquery.CreateXPathNavigator(docs.htmlDoc), nil
	default:
		return nil, fmt.Errorf("an xpath reads an xml or html response, and this one is %s", p.Format)
	}
}

// xpathValue evaluates one selector against the payload: exactly one value.
func xpathValue(p Payload, expr string) (string, error) {
	compiled, err := compiledXPath(expr, p.Namespaces)
	if err != nil {
		return "", fmt.Errorf("xpath %q does not compile: %v", expr, err)
	}
	nav, err := p.navigator()
	if err != nil {
		return "", err
	}
	switch v := compiled.Evaluate(nav).(type) {
	case *xpath.NodeIterator:
		var found []string
		for v.MoveNext() {
			found = append(found, strings.TrimSpace(v.Current().Value()))
			if len(found) > 1 {
				break
			}
		}
		switch len(found) {
		case 0:
			return "", fmt.Errorf("xpath %q selects nothing in the response", expr)
		case 1:
			return found[0], nil
		default:
			n := 2
			for v.MoveNext() {
				n++
			}
			return "", fmt.Errorf("xpath %q selects %d nodes; a selector names exactly one value", expr, n)
		}
	case string:
		return strings.TrimSpace(v), nil
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64), nil
	case bool:
		return strconv.FormatBool(v), nil
	default:
		return "", fmt.Errorf("xpath %q yields no value", expr)
	}
}
