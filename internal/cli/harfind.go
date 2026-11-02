package cli

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/MarcZX10/LinkedinMechaWarrior/internal/errs"
	"github.com/MarcZX10/LinkedinMechaWarrior/internal/har"
	"github.com/MarcZX10/LinkedinMechaWarrior/internal/output"
)

// autoHAR is the file name that asks lmw to find the newest LinkedIn HAR.
const autoHAR = "auto"

// defaultHARPlaces looks where this computer's browser saves files.
func defaultHARPlaces() []har.Place {
	home, _ := os.UserHomeDir()
	wd, _ := os.Getwd()
	return har.Places(har.Env{Home: home, Getenv: os.Getenv, Browser: har.DefaultBrowser(), WorkDir: wd})
}

// askToSearch shows the folders lmw would look in and asks for permission.
func (a *app) askToSearch(places []har.Place) (bool, error) {
	if len(places) == 0 {
		return false, errs.New(errs.Usage, "found no folder to look in; give the HAR file's path instead")
	}
	var list strings.Builder
	w := tabwriter.NewWriter(&list, 2, 4, 2, ' ', 0)
	for _, p := range places {
		fmt.Fprintf(w, "  %s\t%s\n", p.Label, p.Dir)
	}
	_ = w.Flush()
	a.note("lmw will look for HAR files of LinkedIn here. It reads the names of the files\n"+
		"in these folders, the shortcuts to recently saved .har files, and the start of\n"+
		"each .har file, and sends nothing anywhere:\n%s", list.String())
	ok, err := a.confirm("Type 'yes' to let lmw look there: ", "yes")
	if err != nil || !ok {
		a.note("Cancelled; no folder was read.\n")
	}
	return ok, err
}

// findHARs asks for permission and returns the LinkedIn HAR files found.
func (a *app) findHARs() ([]har.Found, bool, error) {
	places := a.harPlaces()
	ok, err := a.askToSearch(places)
	if err != nil || !ok {
		return nil, false, err
	}
	return har.Find(places), true, nil
}

// resolveHAR returns path, or for "auto" the newest LinkedIn HAR file found
// (after asking).
func (a *app) resolveHAR(path string) (string, error) {
	if path != autoHAR {
		return path, nil
	}
	found, ok, err := a.findHARs()
	if err != nil {
		return "", err
	}
	if !ok {
		return "", errs.New(errs.Usage, "no HAR file was chosen")
	}
	if len(found) == 0 {
		return "", errs.New(errs.Usage, "no HAR file of LinkedIn found there. Record one in your browser (see `lmw har --help`) and save it to your downloads")
	}
	a.note("Using %s, saved %s.\n", found[0].Path, found[0].Modified.Format("2006-01-02 15:04"))
	return found[0].Path, nil
}

func (a *app) harFindCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "find",
		Short: "Look for HAR files of LinkedIn where your browser saves files (asks first)",
		Long: `Look for HAR files of LinkedIn, newest first: among your recently saved files
(Windows' Recent items, or recently-used.xbel on Linux), which finds a HAR
wherever you saved it; in the download and last-save folders of your browsers,
the default one first; and in your Downloads and Desktop folders and the
current folder. lmw shows where it will look and asks first; it only reads
file names, shortcuts, and the start of each .har file.

Wherever a command takes a HAR file, "auto" uses the newest one found this
way: lmw auth import --har, lmw har learn auto, lmw har inspect auto.`,
		Example: "  lmw har find\n  lmw auth import --har\n  lmw har learn auto",
		Args:    exactArgs(0, "no arguments"),
		RunE: func(_ *cobra.Command, _ []string) error {
			found, ok, err := a.findHARs()
			if err != nil || !ok {
				return err
			}
			if a.jsonOut {
				if found == nil {
					found = []har.Found{}
				}
				return output.JSON(a.stdout, found)
			}
			if len(found) == 0 {
				a.printf("No HAR file of LinkedIn found. Record one in your browser (see `lmw har --help`).\n")
				return nil
			}
			for i, f := range found {
				a.printf("%d. %s  (%.1f MB, %s)\n", i+1, f.Path, float64(f.Size)/(1<<20), f.Modified.Format("2006-01-02 15:04"))
			}
			return nil
		},
	}
}
