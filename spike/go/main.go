// Spike: does Lip Gloss v2's Compositor answer "which element is under this
// cell?" for nested, overlapping layers — the hit-testing a panel needs.
package main

import (
	"fmt"

	"charm.land/lipgloss/v2"
)

func main() {
	panel := lipgloss.NewLayer(lipgloss.NewStyle().Width(40).Height(10).Render("")).ID("panel")
	save := lipgloss.NewLayer("[ Save ]").ID("save").X(2).Y(8)
	toggle := lipgloss.NewLayer("[x] Name agents").ID("toggle").X(2).Y(1)
	dialog := lipgloss.NewLayer(lipgloss.NewStyle().Width(20).Height(4).Render("discard?")).ID("dialog").X(1).Y(7).Z(5)
	comp := lipgloss.NewCompositor(panel.AddLayers(save, toggle))

	for _, p := range [][2]int{{3, 1}, {4, 8}, {30, 5}, {39, 9}, {40, 10}} {
		fmt.Printf("hit(%d,%d) = %q\n", p[0], p[1], comp.Hit(p[0], p[1]).ID())
	}
	comp.AddLayers(dialog) // an in-panel confirm drawn over the Save button
	fmt.Printf("with dialog on top: hit(4,8) = %q\n", comp.Hit(4, 8).ID())
	fmt.Println(comp.Render())
}
