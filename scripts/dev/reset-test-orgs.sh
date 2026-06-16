#!/usr/bin/env bash
#
# reset-test-orgs.sh — reset the OAuth→OAuth live-test orgs to a known state.
#
#   • WIPES the destination org (gh-oauth-cci-2): deletes every context, every
#     project env var, and every additional project SSH key.
#   • (RE)SEEDS the source org (gh-oauth-cci-1) with a deterministic dataset:
#       - GitHub repos (in BOTH orgs) project-1 / web-app / api-service, each
#         with a trivial .circleci/config.yml
#       - source projects followed (builds enabled) with seeded project env vars
#       - one additional SSH key on web-app (throwaway, generated each run)
#       - contexts: test-1, deploy-prod, and test-2-restriction (the last with a
#         PROJECT restriction to project-1, to exercise restricted-context flows)
#
# Idempotent: safe to run repeatedly. Touches ONLY the two test orgs below.
#
# Requirements:
#   - CIRCLECI_CLI_TOKEN (or CIRCLE_TOKEN): a CircleCI API token whose user is an
#     ADMIN of both orgs.
#   - GITHUB_TOKEN: a GitHub PAT (repo + admin:org) for both GitHub orgs.
#   - python3, ssh-keygen, curl, jq.
#
# Usage:
#   CIRCLECI_CLI_TOKEN=... GITHUB_TOKEN=... scripts/dev/reset-test-orgs.sh
#
set -euo pipefail

SRC_ORG="gh-oauth-cci-1"   # source (seeded)
DST_ORG="gh-oauth-cci-2"   # destination (wiped)
REPOS=(project-1 web-app api-service)
API="https://circleci.com"

TOK="${CIRCLECI_CLI_TOKEN:-${CIRCLE_TOKEN:-}}"
GHT="${GITHUB_TOKEN:-}"
[ -n "$TOK" ] || { echo "ERROR: set CIRCLECI_CLI_TOKEN (or CIRCLE_TOKEN)"; exit 1; }
[ -n "$GHT" ] || { echo "ERROR: set GITHUB_TOKEN"; exit 1; }

cci()    { curl -fsS    -H "Circle-Token: $TOK" "$@"; }
cci_w()  { curl -s -o /dev/null -w '%{http_code}' -H "Circle-Token: $TOK" "$@"; }
ghapi()  { curl -fsS    -H "Authorization: token $GHT" "$@"; }
ghcode() { curl -s -o /dev/null -w '%{http_code}' -H "Authorization: token $GHT" "$@"; }
jqr()    { python3 -c "import json,sys; $1" "${@:2}"; }

echo "==> Resolving org IDs from slugs..."
collab="$(cci "$API/api/v2/me/collaborations")"
SRC_ID="$(printf '%s' "$collab" | jqr "import json,sys;[print(c['id']) for c in json.load(sys.stdin) if c.get('slug')=='gh/'+sys.argv[1]]" "$SRC_ORG")"
DST_ID="$(printf '%s' "$collab" | jqr "import json,sys;[print(c['id']) for c in json.load(sys.stdin) if c.get('slug')=='gh/'+sys.argv[1]]" "$DST_ORG")"
[ -n "$SRC_ID" ] || { echo "ERROR: token cannot see $SRC_ORG"; exit 1; }
[ -n "$DST_ID" ] || { echo "ERROR: token cannot see $DST_ORG"; exit 1; }
echo "    source $SRC_ORG = $SRC_ID"
echo "    dest   $DST_ORG = $DST_ID"

# ── Trivial config committed to each repo so projects are buildable ──────────
CONFIG_B64="$(printf 'version: 2.1\njobs:\n  build:\n    docker:\n      - image: cimg/base:current\n    steps:\n      - checkout\n      - run: echo "building %s"\nworkflows:\n  main:\n    jobs:\n      - build\n' '$CIRCLE_PROJECT_REPONAME' | base64)"

ensure_repo() {  # $1=org $2=repo
  local org="$1" repo="$2"
  if [ "$(ghcode "https://api.github.com/repos/$org/$repo")" = "404" ]; then
    echo "    creating GitHub repo $org/$repo"
    ghapi -X POST -H "Content-Type: application/json" \
      -d "{\"name\":\"$repo\",\"private\":true,\"auto_init\":true,\"description\":\"migration test $repo\"}" \
      "https://api.github.com/orgs/$org/repos" >/dev/null
    sleep 2
  fi
  if [ "$(ghcode "https://api.github.com/repos/$org/$repo/contents/.circleci/config.yml")" = "404" ]; then
    ghapi -X PUT -H "Content-Type: application/json" \
      -d "{\"message\":\"ci: add config\",\"content\":\"$CONFIG_B64\",\"branch\":\"main\"}" \
      "https://api.github.com/repos/$org/$repo/contents/.circleci/config.yml" >/dev/null
  fi
}

echo "==> Ensuring GitHub repos + configs exist in both orgs..."
for org in "$SRC_ORG" "$DST_ORG"; do
  for repo in "${REPOS[@]}"; do ensure_repo "$org" "$repo"; done
done

# ── WIPE destination ─────────────────────────────────────────────────────────
echo "==> Wiping destination org ${DST_ORG}..."
echo "    deleting all contexts"
for cid in $(cci "$API/api/v2/context?owner-id=$DST_ID" | jqr "import json,sys;[print(c['id']) for c in json.load(sys.stdin).get('items',[])]"); do
  cci_w -X DELETE "$API/api/v2/context/$cid" >/dev/null
done
for repo in "${REPOS[@]}"; do
  slug="gh/$DST_ORG/$repo"
  # project env vars
  for name in $(cci "$API/api/v1.1/project/$slug/envvar" 2>/dev/null | jqr "import json,sys;d=json.load(sys.stdin);[print(v['name']) for v in (d if isinstance(d,list) else [])]" 2>/dev/null || true); do
    cci_w -X DELETE "$API/api/v1.1/project/$slug/envvar/$name" >/dev/null
  done
  # additional SSH keys (DELETE needs hostname+fingerprint in the body)
  cci "$API/api/v1.1/project/$slug/settings" 2>/dev/null \
    | jqr "import json,sys;d=json.load(sys.stdin);[print((k.get('hostname') or '')+'\t'+(k.get('fingerprint') or '')) for k in d.get('ssh_keys',[])]" 2>/dev/null \
    | while IFS=$'\t' read -r host fp; do
        [ -n "$fp" ] || continue
        cci_w -X DELETE -H "Content-Type: application/json" \
          -d "{\"hostname\":\"$host\",\"fingerprint\":\"$fp\"}" \
          "$API/api/v1.1/project/$slug/ssh-key" >/dev/null
      done
  echo "    cleared $repo (env vars + ssh keys)"
done

# ── (RE)SEED source ──────────────────────────────────────────────────────────
echo "==> Seeding source org $SRC_ORG..."

# Follow projects (enable builds) + set deterministic project env vars.
set_pvar() { # $1=repo $2=name $3=value
  cci_w -X POST -H "Content-Type: application/json" \
    -d "{\"name\":\"$2\",\"value\":\"$3\"}" \
    "$API/api/v1.1/project/gh/$SRC_ORG/$1/envvar" >/dev/null
}
del_pvars() { # $1=repo — clear existing so the seed is deterministic
  for name in $(cci "$API/api/v1.1/project/gh/$SRC_ORG/$1/envvar" 2>/dev/null | jqr "import json,sys;d=json.load(sys.stdin);[print(v['name']) for v in (d if isinstance(d,list) else [])]" 2>/dev/null || true); do
    cci_w -X DELETE "$API/api/v1.1/project/gh/$SRC_ORG/$1/envvar/$name" >/dev/null
  done
}
for repo in "${REPOS[@]}"; do
  cci_w -X POST "$API/api/v1.1/project/gh/$SRC_ORG/$repo/follow" >/dev/null
done
del_pvars project-1;   set_pvar project-1   APP_ENV     production;     set_pvar project-1   APP_SECRET   "s3cr3t-A1B2"
del_pvars web-app;     set_pvar web-app     DEPLOY_KEY  "deploy-C3D4";  set_pvar web-app     NODE_ENV     production
del_pvars api-service; set_pvar api-service DATABASE_URL "postgres://demo-E5F6"
echo "    projects followed + env vars seeded"

# web-app: ensure one additional SSH key (throwaway, regenerated each run).
have_key="$(cci "$API/api/v1.1/project/gh/$SRC_ORG/web-app/settings" | jqr "import json,sys;print(len(json.load(sys.stdin).get('ssh_keys',[])))")"
if [ "$have_key" = "0" ]; then
  tmpk="$(mktemp -u)"; ssh-keygen -t rsa -b 2048 -m PEM -f "$tmpk" -N "" -q
  pk="$(python3 -c "import json,sys;print(json.dumps(open(sys.argv[1]).read()))" "$tmpk")"
  cci_w -X POST -H "Content-Type: application/json" \
    -d "{\"hostname\":\"github.com\",\"private_key\":$pk}" \
    "$API/api/v1.1/project/gh/$SRC_ORG/web-app/ssh-key" >/dev/null
  rm -f "$tmpk" "$tmpk.pub"
  echo "    web-app SSH key added (github.com)"
else
  echo "    web-app already has $have_key SSH key(s) — left as-is"
fi

# Contexts: recreate the 3 seed contexts fresh (delete if present), set vars,
# and add the project restriction on test-2-restriction.
ctx_id() { cci "$API/api/v2/context?owner-id=$SRC_ID" | jqr "import json,sys;[print(c['id']) for c in json.load(sys.stdin).get('items',[]) if c['name']==sys.argv[1]]" "$1"; }
recreate_ctx() { # $1=name → echoes new context id
  local old; old="$(ctx_id "$1")"
  [ -n "$old" ] && cci_w -X DELETE "$API/api/v2/context/$old" >/dev/null
  cci -X POST -H "Content-Type: application/json" \
    -d "{\"name\":\"$1\",\"owner\":{\"id\":\"$SRC_ID\",\"type\":\"organization\"}}" \
    "$API/api/v2/context" | jqr "import json,sys;print(json.load(sys.stdin)['id'])"
}
set_cvar() { # $1=ctxid $2=name $3=value
  cci_w -X PUT -H "Content-Type: application/json" -d "{\"value\":\"$3\"}" \
    "$API/api/v2/context/$1/environment-variable/$2" >/dev/null
}

c1="$(recreate_ctx test-1)";              set_cvar "$c1" BOO boo-val;        set_cvar "$c1" FOO foo-val
cd_="$(recreate_ctx deploy-prod)";        set_cvar "$cd_" AWS_REGION us-east-1; set_cvar "$cd_" PROD_API_KEY prod-key-7788
cr="$(recreate_ctx test-2-restriction)";  set_cvar "$cr" TEST test-val;       set_cvar "$cr" TEST1 test1-val
echo "    contexts test-1 / deploy-prod / test-2-restriction created + vars set"

# Project restriction on test-2-restriction → project-1 (exercises restricted-context flows).
proj1_uuid="$(cci "$API/api/v2/project/gh/$SRC_ORG/project-1" | jqr "import json,sys;print(json.load(sys.stdin)['id'])")"
cci_w -X POST -H "Content-Type: application/json" \
  -d "{\"restriction_type\":\"project\",\"restriction_value\":\"$proj1_uuid\"}" \
  "$API/api/v2/context/$cr/restrictions" >/dev/null
echo "    test-2-restriction → project restriction to project-1 ($proj1_uuid)"

echo
echo "==> Done. Source ($SRC_ORG) seeded; destination ($DST_ORG) wiped clean."
echo "    Run a migration, e.g.:"
echo "      export CIRCLECI_SOURCE_TOKEN=\$CIRCLECI_CLI_TOKEN CIRCLECI_DEST_TOKEN=\$CIRCLECI_CLI_TOKEN"
echo "      circleci-migrate migrate --source-org github/$SRC_ORG --dest-org github/$DST_ORG \\"
echo "        --transfer-secrets --dest-token-context migration-secrets \\"
echo "        --include-project-vars --include-ssh-keys --remove-restrictions \\"
echo "        --host-project gh/$SRC_ORG/project-1 --apply --yes"
echo "    (first store a $DST_ORG admin token in a '$SRC_ORG' context named 'migration-secrets' → CIRCLECI_DEST_TOKEN)"
