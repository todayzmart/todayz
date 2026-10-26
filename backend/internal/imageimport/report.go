package imageimport

import (
	"fmt"
	"io"
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
