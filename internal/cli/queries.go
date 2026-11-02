package cli

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/MarcZX10/LinkedinMechaWarrior/internal/api"
	"github.com/MarcZX10/LinkedinMechaWarrior/internal/errs"
	"github.com/MarcZX10/LinkedinMechaWarrior/internal/har"
	"github.com/MarcZX10/LinkedinMechaWarrior/internal/output"
)

// loadLearnedQueries reads the GraphQL query versions learned from HAR files.
func loadLearnedQueries(path string) ([]api.LearnedQuery, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var queries []api.LearnedQuery
	if err := json.Unmarshal(raw, &queries); err != nil {
		return nil, errs.Wrap(errs.Storage, err, "%s is corrupted; delete it and run `lmw har learn` again", path)
	}
	return queries, nil
}

// learnQueries saves the queries of a HAR file that lmw uses, replacing older
// versions of the same queries, and returns them.
func (a *app) learnQueries(file *har.File) ([]api.LearnedQuery, error) {
	var found []api.LearnedQuery
	for _, use := range file.Queries() {
		learned := api.LearnedQuery{Name: use.Name, Variables: use.Variables, Hash: use.Hash}
		for _, q := range api.Queries() {
			if q.Matches(learned) {
				found = append(found, learned)
				break
			}
		}
	}
	if len(found) == 0 {
		return nil, nil
	}
	existing, err := loadLearnedQueries(a.cfg.QueriesFile())
	if err != nil {
		return nil, err
	}
	key := func(q api.LearnedQuery) string { return q.Name + "(" + strings.Join(q.Variables, ",") + ")" }
	replaced := map[string]bool{}
	for _, q := range found {
		replaced[key(q)] = true
	}
	var merged []api.LearnedQuery
	for _, q := range existing {
		if !replaced[key(q)] {
			merged = append(merged, q)
		}
	}
	// Later entries are tried first.
	merged = append(merged, found...)
	raw, err := json.MarshalIndent(merged, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(a.cfg.QueriesFile(), raw, 0o600); err != nil {
		return nil, errs.Wrap(errs.Storage, err, "cannot save the learned queries: %v", err)
	}
	api.SetLearnedQueries(merged)
	return found, nil
}

func (a *app) harLearnCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "learn FILE",
		Short: "Learn the current versions of LinkedIn's GraphQL queries from a HAR file",
		Long: `Learn the versions of the GraphQL queries LinkedIn's website uses today.

Messages, comments, reactions, search and members' posts are read through
GraphQL queries whose version (the hash in queryId=NAME.HASH) changes when
LinkedIn updates its website, and then lmw's built-in versions stop working.
When a command says so, record a HAR while the matching page loads (your
inbox, a post's comments, a search...) and give it to this command. lmw keeps
the versions it uses and tries them first from then on.

auth import --har learns them too.`,
		Example: "  lmw har learn linkedin.har",
		Args:    exactArgs(1, "one HAR file"),
		RunE: func(_ *cobra.Command, args []string) error {
			path, err := a.resolveHAR(args[0])
			if err != nil {
				return err
			}
			file, err := har.Load(path)
			if err != nil {
				return err
			}
			learned, err := a.learnQueries(file)
			if err != nil {
				return err
			}
			if a.jsonOut {
				if learned == nil {
					learned = []api.LearnedQuery{}
				}
				return output.JSON(a.stdout, learned)
			}
			if len(learned) == 0 {
				a.printf("No GraphQL queries that lmw uses were found in %s (it has %d). Record the HAR on the page whose command fails.\n",
					args[0], len(file.Queries()))
				return nil
			}
			a.printf("Learned %d query versions:\n", len(learned))
			for _, q := range learned {
				a.printf("  %s(%s) %s\n", q.Name, strings.Join(q.Variables, ", "), q.Hash)
			}
			return nil
		},
	}
}
