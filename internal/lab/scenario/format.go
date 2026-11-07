package scenario

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
)

// WritePlan prints a plan as `lab run --plan` shows it: a headline, the
// systems of each standard per site with each site's floors and totals, and
// the components per product.
func WritePlan(w io.Writer, p *Plan) error {
	label := p.Scenario
	if p.Label != "" {
		label = fmt.Sprintf("%s (%s)", p.Scenario, p.Label)
	}
	if _, err := fmt.Fprintf(w, "scenario %s: %d systems, %d components across %d sites\n\n",
		label, p.Totals.Systems, p.Totals.Components, len(p.Sites)); err != nil {
		return err
	}

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	head := []string{"SITE", "FLOORS"}
	for _, s := range p.Totals.Standards {
		head = append(head, s.Name)
	}
	head = append(head, "SYSTEMS", "COMPONENTS")
	fmt.Fprintln(tw, strings.Join(head, "\t"))
	for _, site := range p.Sites {
		floors := 0
		for _, b := range site.Buildings {
			floors += b.Floors
		}
		fmt.Fprintln(tw, row(site.Name, fmt.Sprint(floors), site.Totals))
	}
	fmt.Fprintln(tw, row("total", "", p.Totals))
	if err := tw.Flush(); err != nil {
		return err
	}

	fmt.Fprintln(w)
	tw = tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "PRODUCT\tCOMPONENTS")
	for _, c := range p.Totals.Products {
		fmt.Fprintf(tw, "%s\t%d\n", c.Name, c.Count)
	}
	return tw.Flush()
}

func row(name, floors string, t Totals) string {
	cells := []string{name, floors}
	for _, s := range t.Standards {
		cells = append(cells, fmt.Sprint(s.Count))
	}
	cells = append(cells, fmt.Sprint(t.Systems), fmt.Sprint(t.Components))
	return strings.Join(cells, "\t")
}
