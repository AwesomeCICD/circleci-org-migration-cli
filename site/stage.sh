#!/usr/bin/env bash
# Stage the repository's Markdown as the Hugo content of the docs site (docs/contribute/docs-site.md).
#
#   site/stage.sh        writes site/content/ (generated, not tracked); then "hugo server" or "hugo --minify" in site/
#
# The Markdown sources are never changed. The pages are the files that site/nav.txt lists (a line with a * lists
# every match, for docs/cli/*.md and the ADRs), the site-only pages of site/pages/ (the plan builder), and README.md,
# which becomes the home page. For every page this script
#   - maps the page-title and page-description front matter keys to title and description, and every other
#     page-* key to params (a page without front matter takes its first heading as the title; the generated
#     docs/cli pages start with a level-two heading and their description is the first line below it),
#   - escapes the "<name>" placeholders of the generated docs/cli pages, which Markdown would read as HTML tags,
#   - removes the first level-one heading, because the theme prints the title,
#   - adds weight and linkTitle from the order and the labels in nav.txt,
#   - rewrites the relative links: a link to another page of the site points to that page, a link to
#     README.md#anchor points to the repository page on GitHub (the home page of the site is not the README),
#     and a link to anything else in the repository (../examples/..., ../.circleci/...) becomes a GitHub blob or
#     tree URL (a raw URL for an image),
# and it writes the _index.md files of the sidebar groups and of the documentation root. A page of site/pages/ goes to
# content/<name>.md (site/pages/plan.md is /plan/); it is not part of the docs tree.
#
# Environment:
#   SITE_REPO_URL   the repository URL for the GitHub links (default: the GitHub link of the menu in hugo.yaml)
#   SITE_REF        the branch or tag of those links (default: main)
#   STAGE_LENIENT=1 report a page that nav.txt misses, or lists but does not find, as a warning, not an error
set -euo pipefail

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
root=$(git -C "${here}" rev-parse --show-toplevel)
content="${here}/content"
ref="${SITE_REF:-main}"
tmp=$(mktemp -d)
trap 'rm -rf "${tmp}"' EXIT

say() { printf 'stage: %s\n' "$*"; }
warn() { printf 'stage: warning: %s\n' "$*" >&2; }
die() { printf 'stage: error: %s\n' "$*" >&2; exit 1; }
# problem MESSAGE: an error, or a warning with STAGE_LENIENT=1
problem() { if [ "${STAGE_LENIENT:-0}" = 1 ]; then warn "$*"; else printf 'stage: error: %s\n' "$*" >&2; failed=1; fi; }
failed=0

# The repository URL of the GitHub links: the GitHub link of the menu in hugo.yaml.
repo="${SITE_REPO_URL:-$(sed -n 's|^[[:space:]]*url:[[:space:]]*\(https://github.com/[^[:space:]]*\)[[:space:]]*$|\1|p' "${here}/hugo.yaml" | head -n 1)}"
repo="${repo%/}"
[ -n "${repo}" ] || die "no GitHub URL in the menu of hugo.yaml: set SITE_REPO_URL"

# nav.txt with every line that names a glob (docs/cli/*.md) replaced by one line for each file, in the C locale order
# (circleci-migrate.md comes before circleci-migrate_export.md). README.md files are never part of a glob. The label of a
# generated command page is its command without the program name ("secrets transfer").
: > "${tmp}/nav.expanded"
while IFS= read -r line || [ -n "${line}" ]; do
  first=${line%%|*}
  first=${first#"${first%%[![:space:]]*}"}
  first=${first%"${first##*[![:space:]]}"}
  case "${first}" in
    '' | '#'* | '=='*) printf '%s\n' "${line}" >> "${tmp}/nav.expanded"; continue ;;
    *'*'*) ;;
    *) printf '%s\n' "${line}" >> "${tmp}/nav.expanded"; continue ;;
  esac
  matches=$(cd "${root}" && LC_ALL=C ls -1 -d ${first} 2> /dev/null | LC_ALL=C sort || true)
  [ -n "${matches}" ] || { problem "nav.txt: ${first} matches no file"; continue; }
  while IFS= read -r f; do
    [ "${f##*/}" != README.md ] || continue
    title=$(sed -n -E 's/^#{1,2} +//p' "${root}/${f}" | head -n 1 | tr -d '`')
    label=""
    case "${title}" in 'circleci-migrate '?*) label=${title#circleci-migrate } ;; esac
    printf '%s | %s\n' "${f}" "${label}" >> "${tmp}/nav.expanded"
  done <<< "${matches}"
done < "${here}/nav.txt"
[ "${failed}" = 0 ] || die "fix site/nav.txt (see its header)"

# nav.expanded -> nav.tsv, fields separated by "|": kind (G group, P page), path or slug, label or title,
# destination (without extension), weight, group slug, description
awk '
  function trim(s) { gsub(/^[ \t]+|[ \t]+$/, "", s); return s }
  function fields(line, a,   n, i, s) {
    n = split(line, a, "|")
    for (i = 1; i <= n; i++) a[i] = trim(a[i])
    return n
  }
  /^[ \t]*(#|$)/ { next }
  /^==/ {
    rest = trim(substr($0, 3)); n = fields(rest, gf); title = gf[1]; grouplead = (n >= 2 ? gf[2] : ""); group = ""
    if (title != "") {
      group = tolower(title); gsub(/[^a-z0-9]+/, "-", group); gsub(/^-|-$/, "", group)
      top++; printf "G|%s|%s|docs/%s|%d|%s\n", group, title, group, top * 10, grouplead
      pages = 0
    } else {
      group = ""
    }
    next
  }
  {
    n = fields($0, pf); path = pf[1]; label = (n >= 2 ? pf[2] : ""); pdesc = (n >= 3 ? pf[3] : "")
    k = split(path, seg, "/"); stem = seg[k]; sub(/\.md$/, "", stem); stem = tolower(stem)
    # docs/contribute/adr/README.md is the page "adr", not "readme"
    if (stem == "readme" && k >= 2) stem = tolower(seg[k - 1])
    if (path ~ /^site\/pages\//) { pages++; w = pages * 10; dest = stem }
    else if (group == "") { top++; w = top * 10; dest = "docs/" stem }
    else { pages++; w = pages * 10; dest = "docs/" group "/" stem }
    printf "P|%s|%s|%s|%d|%s|%s\n", path, label, dest, w, group, pdesc
  }
' "${tmp}/nav.expanded" > "${tmp}/nav.tsv"

# The site-only pages that nav.txt does not list are staged too, at their own place, without a label (group "-": no list).
for f in "${root}"/site/pages/*.md; do
  [ -f "${f}" ] || continue
  rel="site/pages/${f##*/}"
  awk -F'|' -v p="${rel}" '$1 == "P" && $2 == p { found = 1 } END { exit !found }' "${tmp}/nav.tsv" && continue
  stem=${f##*/}; stem=${stem%.md}
  printf 'P|%s||%s||-|\n' "${rel}" "$(printf '%s' "${stem}" | tr '[:upper:]' '[:lower:]')" >> "${tmp}/nav.tsv"
done

# Every listed page must exist, and every docs page must be listed.
while IFS='|' read -r kind path _; do
  [ "${kind}" = P ] || continue
  [ -f "${root}/${path}" ] || problem "nav.txt lists ${path}, which does not exist"
done < "${tmp}/nav.tsv"
while IFS= read -r f; do
  rel=${f#"${root}"/}
  # docs/cli/README.md is the index that cobra writes: the CLI reference group has its own.
  [ "${rel}" != docs/cli/README.md ] || continue
  awk -F'|' -v p="${rel}" '$1 == "P" && $2 == p { found = 1 } END { exit !found }' "${tmp}/nav.tsv" \
    || problem "${rel} is not listed in site/nav.txt, so it would be missing from the site"
done < <(find "${root}/docs" -name '*.md' -type f | LC_ALL=C sort)
[ "${failed}" = 0 ] || die "fix site/nav.txt (see its header)"

# Where every page goes (README.md is the home page), and every file and directory the links may point to.
# map.tsv: a repository path and the place of the page in the site (README.md is handled apart: the home page shows no README).
# A directory of the docs becomes the index of its group, and docs/cli the index of the CLI reference.
{
  # A page of site/pages/ is also reached by its URL (../../plan/ of a docs page, which GitHub shows as a broken link: write the full URL of the site there).
  awk -F'|' '$1 == "P" { printf "%s\t%s.md\n", $2, $4; if ($2 ~ /^site\/pages\//) printf "%s\t%s.md\n", $4, $4 } $1 == "G" { printf "docs/%s\tdocs/%s/\n", $2, $2 }' "${tmp}/nav.tsv"
  printf 'docs\tdocs/\n'
  printf 'docs/cli\tdocs/cli-reference/\n'
  printf 'docs/cli/README.md\tdocs/cli-reference/\n'
} > "${tmp}/map.tsv"
git -C "${root}" ls-files | awk '
  { print "F\t" $0; n = split($0, a, "/"); d = ""; for (i = 1; i < n; i++) { d = d (i > 1 ? "/" : "") a[i]; seen[d] = 1 } }
  END { for (d in seen) print "D\t" d }' > "${tmp}/paths.tsv"

# The transformation of one page: stage MARKDOWN-FILE DESTINATION WEIGHT LABEL [NAV-DESCRIPTION]
transform() {
  awk -v src="$1" -v dest="$2" -v weight="$3" -v label="$4" -v navdesc="${5:-}" -v repo="${repo}" -v ref="${ref}" \
    -v mapfile="${tmp}/map.tsv" -v pathsfile="${tmp}/paths.tsv" -v manifest="${tmp}/pages.tsv" '
    function trim(s) { gsub(/^[ \t]+|[ \t]+$/, "", s); return s }
    function unquote(s) { if (s ~ /^".*"$/ || s ~ /^\047.*\047$/) s = substr(s, 2, length(s) - 2); return s }
    function yq(s) { gsub(/\\/, "\\\\", s); gsub(/"/, "\\\"", s); return "\"" s "\"" }
    function dirname(p) { if (p !~ /\//) return ""; sub(/\/[^\/]*$/, "", p); return p }
    function normalize(path,   n, parts, out, i, k, s) {
      n = split(path, parts, "/"); k = 0
      for (i = 1; i <= n; i++) {
        if (parts[i] == "" || parts[i] == ".") continue
        if (parts[i] == "..") { if (k == 0) return "../"; k--; continue }
        out[++k] = parts[i]
      }
      s = ""; for (i = 1; i <= k; i++) s = s (i > 1 ? "/" : "") out[i]
      return s
    }
    # A link target: a page of the site -> its place in the site, anything else in the repository -> GitHub.
    function rewrite(url,   frag, i, rp, base) {
      if (url == "" || url ~ /^[A-Za-z][A-Za-z0-9+.-]*:/ || url ~ /^[#\/<]/) return url
      frag = ""; i = index(url, "#")
      if (i) { frag = substr(url, i); url = substr(url, 1, i - 1) }
      base = dirname(src)
      rp = normalize((base == "" ? "" : base "/") url)
      if (rp == "../") { print "stage: warning: " src ": link leaves the repository: " url > "/dev/stderr"; return url frag }
      # The home page of the site is not the README: its anchors are on the repository page, which shows the README.
      if (rp == "README.md") return (frag == "" ? "/" : repo frag)
      if (rp in pages) return "/" pages[rp] frag
      if (rp == "") return repo "/tree/" ref
      if (rp in isdir) return repo "/tree/" ref "/" rp frag
      if (!(rp in isfile)) print "stage: warning: " src ": link to a file that git does not track: " url > "/dev/stderr"
      return repo "/blob/" ref "/" rp frag
    }
    # The target of an image: a file of the repository that the page cannot reference, so its raw URL on GitHub.
    function rawimage(url,   rp, base) {
      if (url == "" || url ~ /^[A-Za-z][A-Za-z0-9+.-]*:/ || url ~ /^[#\/<]/) return url
      base = dirname(src)
      rp = normalize((base == "" ? "" : base "/") url)
      if (rp == "../" || rp == "") return url
      return repo "/raw/" ref "/" rp
    }
    function fixlinks(s,   out, pre, inner, dst, rest, sp, after, k, isimg) {
      out = ""
      while (match(s, /\]\([^)]*\)/)) {
        pre = substr(s, 1, RSTART - 1); inner = substr(s, RSTART + 2, RLENGTH - 3); after = substr(s, RSTART + RLENGTH)
        sp = index(inner, " ")
        if (sp) { dst = substr(inner, 1, sp - 1); rest = substr(inner, sp) } else { dst = inner; rest = "" }
        # An image: the "[" that opens this link text follows a "!".
        isimg = 0
        for (k = length(pre); k >= 1; k--) if (substr(pre, k, 1) == "[") { isimg = (k > 1 && substr(pre, k - 1, 1) == "!"); break }
        out = out pre "](" (isimg ? rawimage(dst) : rewrite(dst)) rest ")"
        s = after
      }
      return out s
    }
    # The "<name>" placeholders of the generated command pages are text, not HTML: &lt; (in code spans and fences too, where it is wrong, so not there).
    function escapeangle(s,   n, parts, i, out) {
      if (index(s, "<") == 0) return s
      n = split(s, parts, "`"); out = ""
      for (i = 1; i <= n; i++) { if (i % 2 == 1) gsub(/</, "\\&lt;", parts[i]); out = out (i > 1 ? "`" : "") parts[i] }
      return out
    }
    # A line outside code fences: links outside of code spans only.
    function fixline(s,   n, parts, i, out) {
      if (index(s, "](") == 0) return s
      n = split(s, parts, "`"); out = ""
      for (i = 1; i <= n; i++) out = out (i > 1 ? "`" : "") (i % 2 == 1 ? fixlinks(parts[i]) : parts[i])
      return out
    }
    BEGIN {
      while ((getline line < mapfile) > 0) { split(line, a, "\t"); pages[a[1]] = a[2] }
      while ((getline line < pathsfile) > 0) { split(line, a, "\t"); if (a[1] == "F") isfile[a[2]] = 1; else isdir[a[2]] = 1 }
      lines = 0; infm = 0; infence = 0; h1done = 0; skipblank = 0; params = ""; extra = ""
      # The generated command pages (docs/cobra) have no front matter, start with a level-two heading and use "<name>" placeholders.
      generated = (src ~ /^docs\/cli\//)
      prevblank = 1; incode = 0; inlist = 0; gotdesc = 0
    }
    NR == 1 && $0 == "---" { infm = 1; next }
    infm {
      if ($0 == "---") { infm = 0; next }
      if (match($0, /^[A-Za-z][A-Za-z0-9_-]*:/)) {
        key = substr($0, 1, RLENGTH - 1); val = trim(substr($0, RLENGTH + 1))
        if (key == "page-title" || key == "title") title = unquote(val)
        else if (key == "page-description") description = unquote(val)
        else if (key == "description") description = unquote(val)
        else if (key ~ /^page-/) params = params "  " substr(key, 6) ": " yq(unquote(val)) "\n"
        else { if (key == "weight") fmweight = 1; if (key == "linkTitle") fmlink = 1; extra = extra $0 "\n" }
      } else extra = extra $0 "\n"
      next
    }
    {
      if (skipblank) { skipblank = 0; if ($0 ~ /^[ \t]*$/) next }
      if (lines == 0 && $0 ~ /^[ \t]*$/) next
      if (match($0, /^ *(```+|~~~+)/)) {
        mk = $0; sub(/^ */, "", mk); ch = substr(mk, 1, 1); len = 0
        while (substr(mk, len + 1, 1) == ch) len++
        if (!infence) { infence = 1; fch = ch; flen = len }
        else if (ch == fch && len >= flen && mk ~ /^(`+|~+)[ \t]*$/) infence = 0
        body[++lines] = $0; prevblank = 0; next
      }
      if (infence) { body[++lines] = $0; next }
      if (!h1done && ($0 ~ /^# / || (generated && title == "" && $0 ~ /^## /))) {
        h1done = 1; skipblank = 1; h1 = $0; sub(/^#+ /, "", h1); gsub(/`/, "", h1); next
      }
      # The first line of text below the title heading is the description of a generated page.
      if (generated && h1done && !gotdesc && $0 !~ /^[ \t]*$/ && $0 !~ /^#/) {
        gotdesc = 1; gd = trim($0); gsub(/`/, "", gd); if (description == "" && gd !~ /[<>]/) description = gd
      }
      if (generated) {
        # Where an indented block is code, "<" stays as it is; in a paragraph or a list it is escaped.
        if ($0 ~ /^[ \t]*$/) { prevblank = 1; body[++lines] = $0; next }
        indented = ($0 ~ /^(    |\t)/)
        if ($0 ~ /^[ \t]*([0-9]+[.)]|[-*+])[ \t]/) { inlist = 1; incode = 0 }
        else if ($0 !~ /^[ \t]/) { inlist = 0; incode = 0 }
        else if (!incode && prevblank && indented && !inlist) incode = 1
        else if (incode && !indented) incode = 0
        prevblank = 0
        body[++lines] = incode ? $0 : escapeangle(fixline($0))
        next
      }
      body[++lines] = fixline($0)
    }
    END {
      if (title == "") title = h1
      if (title == "") print "stage: warning: " src ": no title" > "/dev/stderr"
      if (description == "" && navdesc != "") description = navdesc
      print "---"
      print "title: " yq(title)
      if (label != "" && !fmlink) print "linkTitle: " yq(label)
      if (description != "") print "description: " yq(description)
      if (weight != "" && !fmweight) print "weight: " weight
      if (params != "") { print "params:"; printf "%s", params }
      printf "%s", extra
      print "---"; print ""
      for (i = 1; i <= lines; i++) print body[i]
      printf "%s\t%s\t%s\n", dest, (label != "" ? label : title), description >> manifest
    }
  ' "${root}/$1"
}

rm -rf "${content}"
mkdir -p "${content}/docs"
: > "${tmp}/pages.tsv"

home_description=$(
  awk '
    /^params:/ { inparams = 1; next }
    inparams && /^  description:/ {
      sub(/^  description:[ \t]*/, "")
      gsub(/^"|"$/, "")
      print
      exit
    }
    inparams && /^[^ \t#]/ { exit }
  ' "${here}/hugo.yaml"
)
transform README.md _index "" "" > "${content}/_index.md"
if [ -n "${home_description}" ] && ! grep -q '^description:' "${content}/_index.md"; then
  awk -v d="${home_description}" '
    BEGIN { done = 0 }
    /^title:/ && !done { print; print "description: \"" d "\""; done = 1; next }
    { print }
  ' "${content}/_index.md" > "${content}/_index.md.tmp" && mv "${content}/_index.md.tmp" "${content}/_index.md"
fi
while IFS='|' read -r kind path label dest weight _group navdesc; do
  [ "${kind}" = P ] || continue
  [ -f "${root}/${path}" ] || continue  # reported above (a warning with STAGE_LENIENT=1)
  mkdir -p "${content}/$(dirname "${dest}")"
  transform "${path}" "${dest}" "${weight}" "${label}" "${navdesc:-}" > "${content}/${dest}.md"
done < "${tmp}/nav.tsv"

# The index pages: one for each group and one for the documentation root, each with a list of its pages.
# list GROUP: the pages of a group (GROUP empty: the top-level pages), as Markdown bullets.
list() {
  awk -F'|' -v g="$1" -v pagesfile="${tmp}/pages.tsv" '
    BEGIN { while ((getline line < pagesfile) > 0) { split(line, a, "\t"); title[a[1]] = a[2]; desc[a[1]] = a[3] } }
    $1 == "P" && $6 == g {
      printf "- [%s](/%s.md)", ($3 != "" ? $3 : title[$4]), $4
      if (desc[$4] != "") printf ": %s", desc[$4]
      printf "\n"
    }' "${tmp}/nav.tsv"
}
cut155() {
  awk -v s="$1" '
    function cut155(x) {
      if (length(x) <= 155) return x
      x = substr(x, 1, 155)
      sub(/[ \t][^ \t]*$/, "", x)
      return x "…"
    }
    BEGIN { print cut155(s) }
  '
}

group_description() {
  local lead firstdest d
  lead=$(awk -F'|' -v g="$1" '$1 == "G" && $2 == g { print $6; exit }' "${tmp}/nav.tsv")
  if [ -n "${lead}" ]; then cut155 "${lead}"; return; fi
  firstdest=$(awk -F'|' -v g="$1" '$1 == "P" && $6 == g { print $4; exit }' "${tmp}/nav.tsv")
  [ -n "${firstdest}" ] || return 0
  d=$(awk -F'\t' -v d="${firstdest}" '$1 == d { print $3; exit }' "${tmp}/pages.tsv")
  if [ -n "${d}" ]; then cut155 "${d}"; fi
}
{
  printf -- '---\ntitle: Documentation\ndescription: "Choose your migration, follow one runbook with copy-paste commands, and look up every command and flag of circleci-migrate."\n---\n\nNot sure where to start? Build your plan in the [plan builder](/plan.md), or read [Choose a migration](/docs/get-started/choose.md). Then use the groups below.\n\n'
  while IFS='|' read -r kind slug title _dest _weight _lead; do
    case "${kind}" in
      G) printf '## %s\n\n' "${title}"; list "${slug}"; printf '\n' ;;
    esac
  done < "${tmp}/nav.tsv"
  top=$(list "")
  if [ -n "${top}" ]; then printf '## More\n\n%s\n' "${top}"; fi
} > "${content}/docs/_index.md"
while IFS='|' read -r kind slug title dest weight _lead; do
  [ "${kind}" = G ] || continue
  mkdir -p "${content}/${dest}"
  gdesc=$(group_description "${slug}")
  if [ -n "${gdesc}" ]; then
    printf -- '---\ntitle: "%s"\nweight: %s\ndescription: "%s"\n---\n\n' "${title}" "${weight}" "${gdesc}" > "${content}/${dest}/_index.md"
  else
    printf -- '---\ntitle: "%s"\nweight: %s\n---\n\n' "${title}" "${weight}" > "${content}/${dest}/_index.md"
  fi
  list "${slug}" >> "${content}/${dest}/_index.md"
done < "${tmp}/nav.tsv"

say "staged $(find "${content}" -name '*.md' | wc -l | tr -d ' ') Markdown files in ${content#"${root}"/} (repository ${repo}, ref ${ref})"
