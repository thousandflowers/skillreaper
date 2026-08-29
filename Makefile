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
#
# The hero block opens with the ASCII wordmark, which reap does not print. It
# lives in docs/wordmark.txt and is prepended here, so the whole fence stays
# generated — a :start marker placed above a hand-kept wordmark would delete it
# on the first run.
# The badge carries its own disclaimer, inside the fence. The distinction lived
# in a <sub> line under the block and in the prose beside it, and neither travels:
# a reader who sees "1% utilization" at the top and "3.4%" further down concludes
# one of them is wrong, and the badge is the one that gets screenshotted, linked
# and pasted into a chat with none of its neighbours. A number that cannot be
# reproduced has to say so where it is read.
SAMPLE_NOTE = a generated sample stack, not a real install - run reap for your own numbers

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
	{ cat docs/wordmark.txt; echo '$(SAMPLE_NOTE)'; echo; reap -agent | trim; } > "$$dir/numbers.txt"; \
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
# CAPTURE replays a stored run instead of measuring again:
#
#   make readme-mine CAPTURE=docs/measurements/2026-08-23.json
#
# That is how the published figures stay the ones docs/measurements holds and the
# portfolio quotes. Re-measuring moves them, and a page that cites a run nobody
# can reproduce is the failure this whole target exists to avoid.
#
#   make readme-mine
#   make readme-mine && git diff --exit-code README.md   # stable within a day
.PHONY: readme-mine
readme-mine:
	@set -eu; \
	json="$${CAPTURE:-}"; \
	if [ -n "$$json" ]; then echo "replaying $$json"; else \
		json="$$(mktemp)"; go run ./cmd/reap -json -no-nudge > "$$json"; fi; \
	test -s "$$json" || { echo "reap produced no JSON" >&2; exit 1; }; \
	go run ./internal/readme README.md < "$$json"; \
	echo "README.md: figures regenerated from this machine's own transcripts"

# Regenerate the before/after captures under docs/renders from two binaries:
# "before" built from a released tag, "after" from this working tree, both run
# against the same generated fixture.
#
# The fixture is the control, not a compromise. These captures are evidence
# about the FORM of the report - what a redesign did to it - so the input has to
# be identical on both sides for the comparison to isolate that variable. A
# before and an after taken against two different stacks measures the stacks.
# My own numbers are evidence of a different thing, the finding itself, and they
# live in docs/measurements and in the README's "my install" block.
#
#   make renders                       # rewrite docs/renders/*.txt
#   make renders BEFORE_REF=v0.6.3     # against an older release
#   make renders-check                 # fail if the committed captures drifted
#
# renders-check exists because generated evidence that nothing re-checks goes
# stale in silence: the previous captures kept showing a headline the binary had
# stopped printing, and nothing in the repo could notice. Run it before tagging.
BEFORE_REF ?= v0.7.0

.PHONY: renders
renders:
	@docs/renders/capture.sh docs/renders "$(BEFORE_REF)"

.PHONY: renders-check
renders-check:
	@set -eu; \
	tmp="$$(mktemp -d)"; \
	trap 'rm -rf "$$tmp"' EXIT; \
	docs/renders/capture.sh "$$tmp" "$(BEFORE_REF)" >/dev/null; \
	if diff -ru docs/renders "$$tmp" --exclude='*.md' --exclude='*.sh'; then \
		echo "docs/renders: captures match the binaries"; \
	else \
		echo "docs/renders is stale — run: make renders" >&2; exit 1; \
	fi
