# Regenerate every measured block in README.md from a generated sample stack, so
# the page cannot drift from what reap actually prints, and so the blocks cannot
# drift from each other: all three come from one run against one fixture.
#
# Idempotent by construction: each block between its two markers is replaced
# wholesale, so running the target twice leaves README.md byte-identical.
#
#   make readme-numbers
#   make readme-numbers && git diff --exit-code README.md   # must be clean
#
# Only the fenced blocks are generated. The prose figures under "Two costs of
# context bloat" are measured on the maintainer's own stack and cannot be
# reproduced from a fixture, so they stay hand-maintained on purpose.
.PHONY: readme-numbers
readme-numbers:
	@set -eu; \
	dir="$$(mktemp -d)/hero-claude"; \
	docs/gif-helpers/hero-fixture.sh "$$dir" >/dev/null; \
	reap() { go run ./cmd/reap -claude-dir "$$dir" -claude-json "$$dir/.claude.json" \
		-no-nudge -no-color "$$@"; }; \
	trim() { awk 'NF { seen = 1 } seen { buf[++n] = $$0 } \
		END { while (n > 0 && buf[n] ~ /^[[:space:]]*$$/) n--; \
		      for (i = 1; i <= n; i++) print buf[i] }'; }; \
	replace() { \
		test -s "$$2" || { echo "reap produced no output for $$1" >&2; exit 1; }; \
		grep -q "^<!-- readme-$$1:start -->$$" README.md || \
			{ echo "README.md has no readme-$$1 markers — nothing to replace" >&2; exit 1; }; \
		awk -v f="$$2" -v m="$$1" ' \
			$$0 == "<!-- readme-" m ":start -->" { \
				print; print "```"; \
				while ((getline l < f) > 0) print l; \
				print "```"; inside = 1; next } \
			$$0 == "<!-- readme-" m ":end -->" { inside = 0 } \
			!inside { print }' README.md > "$$dir/README.md"; \
		cat "$$dir/README.md" > README.md; \
	}; \
	reap -agent | trim > "$$dir/numbers.txt"; \
	reap gap | trim > "$$dir/gap.txt"; \
	reap | grep utilization | trim > "$$dir/utilization.txt"; \
	replace numbers "$$dir/numbers.txt"; \
	replace gap "$$dir/gap.txt"; \
	replace utilization "$$dir/utilization.txt"; \
	echo "README.md: measured blocks regenerated"

# MAINTAINER ONLY — do not run this unless the README's "measured on my own
# setup" figures are supposed to become *your* figures.
#
# readme-numbers above regenerates the sample-stack blocks and is what a
# contributor runs; it only ever touches readme-numbers / readme-gap /
# readme-utilization. This target only ever touches readme-mine-*. The two sets
# cannot overwrite each other, so a contributor running the normal target can
# never replace a real measurement with fixture data.
#
# One run, decoded once, every figure derived from it — mixing two runs is how
# the page ended up carrying two different totals for the same stack.
#
#   make readme-mine
#   make readme-mine && git diff --exit-code README.md   # stable within a day
.PHONY: readme-mine
readme-mine:
	@set -eu; \
	json="$$(mktemp)"; \
	go run ./cmd/reap -json -no-nudge > "$$json"; \
	test -s "$$json" || { echo "reap produced no JSON" >&2; exit 1; }; \
	go run ./internal/readme README.md < "$$json"; \
	echo "README.md: figures regenerated from this machine's own transcripts"
