package main

import (
	"flag"
	"fmt"
	"strings"
)

// commandRule says what one subcommand takes. A flag or an argument a command
// has no use for used to be dropped in silence: `reap mute a b` muted a, and
// `reap restore 001 --all` restored everything. On an action that is the wrong
// thing done quietly, so anything unusable is refused with exit 2 before work
// starts.
type commandRule struct {
	maxArgs int
	flags   string // the flags of restrictedFlags this command takes
}

// restrictedFlags are the ones that change what a command does or prints. The
// rest (--days, --claude-dir and so on) tune the shared scan and mean the same
// everywhere.
const restrictedFlags = "json md agent quiet yes all dry-run source list remove"

var commandRules = map[string]commandRule{
	"":               {0, "json md agent quiet"},
	"gap":            {0, "json md agent"},
	"by-project":     {0, "json"},
	"route":          {0, "json md"},
	"apm":            {0, "json md"},
	"manifest":       {1, "json"},
	"why":            {1, "json"},
	"keep":           {1, "list remove"},
	"mute":           {1, "json quiet yes all source"},
	"unmute":         {1, "all yes"},
	"prune":          {1, "json quiet yes source"},
	"restore":        {1, "all yes"},
	"delete":         {1, "yes"},
	"install-hook":   {0, "dry-run"},
	"uninstall-hook": {0, ""},
	"share":          {0, "json md"},
	"snapshot":       {0, "json list"},
	"diff":           {2, "json"},
	"nudge":          {0, ""},
	"version":        {0, ""},
	"help":           {0, ""},
}

// invocationError says what is wrong with this command line, or "" if nothing
// is. Unknown commands are left to the dispatch below it.
func invocationError(fs *flag.FlagSet, cmd string, rest []string) string {
	rule, known := commandRules[cmd]
	if !known {
		return ""
	}
	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
	label := strings.TrimSpace("reap " + cmd)

	for _, name := range strings.Fields(restrictedFlags) {
		if set[name] && !strings.Contains(" "+rule.flags+" ", " "+name+" ") {
			return fmt.Sprintf("%s does not take --%s", label, name)
		}
	}
	if len(rest) > rule.maxArgs {
		return fmt.Sprintf("%s got too many arguments: %s", label, strings.Join(rest[rule.maxArgs:], " "))
	}
	named := len(rest) > 0
	switch {
	case set["all"] && named:
		return fmt.Sprintf("%s takes a name or --all, not both", label)
	case set["source"] && named:
		return fmt.Sprintf("%s takes a name or --source, not both", label)
	case set["list"] && (named || set["remove"]):
		return fmt.Sprintf("%s --list takes nothing else", label)
	case set["remove"] && named:
		return fmt.Sprintf("%s takes a name or --remove <name>, not both", label)
	case set["remove"] && strings.HasPrefix(fs.Lookup("remove").Value.String(), "-"):
		return "--remove needs a name, not a flag"
	}
	// `reap --source prune --yes` reads prune as the flag's value, so the run
	// that was asked to change things prints a report and says it succeeded.
	if cmd == "" {
		var swallowed string
		fs.Visit(func(f *flag.Flag) {
			if _, isCmd := commandRules[f.Value.String()]; isCmd && f.Value.String() != "" {
				swallowed = fmt.Sprintf("--%s took %q as its value, so no command ran. Give --%s a value, or put the command first", f.Name, f.Value, f.Name)
			}
		})
		return swallowed
	}
	return ""
}
