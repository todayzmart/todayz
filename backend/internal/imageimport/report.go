package imageimport

import (
	"fmt"
	"io"
	"strings"
)

// WriteMarkdown writes the report as a Markdown page for the founder and
// the admin who reviews the drafts.
func (r Report) WriteMarkdown(w io.Writer) error {
	run := "Import"
	if r.DryRun {
		run = "Dry run (nothing stored)"
	}
	if _, err := fmt.Fprintf(w, "# Product image import\n\n%s: %d products tried; %d found, %d not found, %d medicines skipped, %d with nothing to look up, %d failed.\n",
		run, r.Tried, len(r.Imported), len(r.Missed), len(r.Medicines), len(r.NoKey), len(r.Failed)); err != nil {
		return err
	}
	sections := []struct {
		title string
		lines []Line
		found bool
	}{
		{"Found (waiting for admin approval)", r.Imported, true},
		{"Not found", r.Missed, false},
		{"Medicines (stay on the placeholder)", r.Medicines, false},
		{"No barcode and no brand", r.NoKey, false},
		{"Failed", r.Failed, false},
	}
	for _, s := range sections {
		if len(s.lines) == 0 {
			continue
		}
		if _, err := fmt.Fprintf(w, "\n## %s\n\n", s.title); err != nil {
			return err
		}
		for _, l := range s.lines {
			var err error
			switch {
			case s.found:
				_, err = fmt.Fprintf(w, "- %s: %s by %s, %s\n", l.Name, l.Source, l.Match, l.PageURL)
			case l.Reason != "":
				_, err = fmt.Fprintf(w, "- %s: %s\n", l.Name, l.Reason)
			default:
				_, err = fmt.Fprintf(w, "- %s\n", l.Name)
			}
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// WriteMarkdown writes an `images types` run as a Markdown page.
func (r TypeReport) WriteMarkdown(w io.Writer) error {
	run := "Run"
	if r.DryRun {
		run = "Dry run (nothing stored)"
	}
	cleared := ""
	if r.Cleared > 0 {
		cleared = fmt.Sprintf("%d waiting candidates dropped to search again; ", r.Cleared)
	}
	if _, err := fmt.Fprintf(w, "# Item-type photos\n\n%s: %d products given their kind's approved photo; %s%d kinds searched; %d with candidates, %d with none, %d medicines skipped, %d failed.\n",
		run, r.Filled, cleared, r.Searched, len(r.Found), len(r.Missed), len(r.Medicines), len(r.Failed)); err != nil {
		return err
	}
	sections := []struct {
		title string
		lines []TypeLine
	}{
		{"Candidates (waiting for an admin to pick one in A-08)", r.Found},
		{"No free-licence photo found", r.Missed},
		{"Medicines (stay on the placeholder)", r.Medicines},
		{"Failed", r.Failed},
	}
	for _, s := range sections {
		if len(s.lines) == 0 {
			continue
		}
		if _, err := fmt.Fprintf(w, "\n## %s\n\n", s.title); err != nil {
			return err
		}
		for _, l := range s.lines {
			line := fmt.Sprintf("- %s (%s, %d products)", l.TypeKey, l.Category, l.Products)
			switch {
			case len(l.Files) > 0:
				line += ": " + strings.Join(l.Files, "; ")
			case l.Reason != "":
				line += ": " + l.Reason
			}
			if _, err := fmt.Fprintln(w, line); err != nil {
				return err
			}
		}
	}
	return nil
}

// WriteMarkdown writes an `images types --picks` run as a Markdown page.
func (r PickReport) WriteMarkdown(w io.Writer) error {
	run := "Run"
	if r.DryRun {
		run = "Dry run (nothing stored; each file checked on Commons)"
	}
	products := 0
	for _, l := range r.Picked {
		products += l.Products
	}
	if _, err := fmt.Fprintf(w, "# Item-type photo picks\n\n%s: %d products given their kind's approved photo first; %d picks applied (%d products), %d left alone, %d failed.\n",
		run, r.Filled, len(r.Picked), products, len(r.Kept), len(r.Failed)); err != nil {
		return err
	}
	sections := []struct {
		title string
		lines []TypeLine
	}{
		{"Applied", r.Picked},
		{"Left alone", r.Kept},
		{"Failed (pick these again in A-08)", r.Failed},
	}
	for _, s := range sections {
		if len(s.lines) == 0 {
			continue
		}
		if _, err := fmt.Fprintf(w, "\n## %s\n\n", s.title); err != nil {
			return err
		}
		for _, l := range s.lines {
			line := fmt.Sprintf("- %s (%s, %d products): %s", l.TypeKey, l.Category, l.Products, strings.Join(l.Files, "; "))
			if l.Reason != "" {
				line += " — " + l.Reason
			}
			if _, err := fmt.Fprintln(w, line); err != nil {
				return err
			}
		}
	}
	return nil
}
