package scenario

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
)

// WritePlan prints a plan as `lab run --plan` shows it: a headline, the
// systems of each standard per archetype with its sites, floors and totals,
// each site's systems and components, the components per product, and the
// pushers per profile when the scenario declares any. The
// per-site table is narrow, so it reads at the scenario's most sites.
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
	head := []string{"ARCHETYPE", "SITES", "FLOORS"}
	for _, s := range p.Totals.Standards {
		head = append(head, s.Name)
	}
	head = append(head, "SYSTEMS", "COMPONENTS")
	fmt.Fprintln(tw, strings.Join(head, "\t"))
	for _, a := range p.Archetypes {
		fmt.Fprintln(tw, row(a.Name, fmt.Sprint(a.Sites), fmt.Sprint(a.Floors), a.Totals))
	}
	fmt.Fprintln(tw, row("total", fmt.Sprint(len(p.Sites)), "", p.Totals))
	if err := tw.Flush(); err != nil {
		return err
	}

	fmt.Fprintln(w)
	tw = tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "SITE\tARCHETYPE\tSYSTEMS\tCOMPONENTS")
	for _, s := range p.Sites {
		fmt.Fprintf(tw, "%s\t%s\t%d\t%d\n", s.Name, s.Archetype, s.Totals.Systems, s.Totals.Components)
	}
	if err := tw.Flush(); err != nil {
		return err
	}

	fmt.Fprintln(w)
	tw = tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "PRODUCT\tCOMPONENTS")
	for _, c := range p.Totals.Products {
		fmt.Fprintf(tw, "%s\t%d\n", c.Name, c.Count)
	}
	if err := tw.Flush(); err != nil || len(p.Pushers) == 0 {
		return err
	}

	// The pushers by profile: how many, never a token (#1743).
	fmt.Fprintln(w)
	tw = tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "PUSHER\tCOUNT")
	for _, c := range p.Pushers {
		fmt.Fprintf(tw, "%s\t%d\n", c.Name, c.Count)
	}
	return tw.Flush()
}

func row(name, sites, floors string, t Totals) string {
	cells := []string{name, sites, floors}
	for _, s := range t.Standards {
		cells = append(cells, fmt.Sprint(s.Count))
	}
	cells = append(cells, fmt.Sprint(t.Systems), fmt.Sprint(t.Components))
	return strings.Join(cells, "\t")
}
