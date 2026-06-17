#!/usr/bin/env bash
#
# reset-test-orgs.sh — reset the OAuth→OAuth live-test orgs to a known state.
#
#   • WIPES the destination org (gh-oauth-cci-2): deletes every context, every
#     project env var, every additional project SSH key, and resets project
#     feature flags and the config-policy bundle to defaults.
#   • (RE)SEEDS the source org (gh-oauth-cci-1) with a deterministic dataset:
#       - GitHub repos (in BOTH orgs) project-1 / web-app / api-service, each
#         with a trivial .circleci/config.yml
#       - source projects followed (builds enabled) with seeded project env vars
#       - one additional SSH key on web-app (throwaway, generated each run)
#       - contexts: test-1, deploy-prod, and test-2-restriction (the last with a
#         PROJECT restriction to project-1, to exercise restricted-context flows)
#       - org orb settings (allow-uncertified-public-orbs + allow-private-orbs)
#         enabled on BOTH orgs so orb create/publish/sync work
#       - config-policy bundle on source (sample Rego policy); cleared on dest
#       - runner resource class gh-oauth-cci-1/linux-x64 on source (idempotent);
#         all runner resource classes wiped from dest namespace gh-oauth-cci-2
#       - orbs on source: gh-oauth-cci-1/demo-orb (public, 3 versions) and
#         gh-oauth-cci-1/demo-private (private, 1 version) — requires circleci
#         CLI on PATH; skipped (with warning) if absent; dest orb state is NOT
#         reset (orb versions are immutable)
#       - api-service project feature flags set to non-defaults on source
#         (oss, build-fork-prs, autocancel-builds=true) and reset on dest
#
# Idempotent: safe to run repeatedly. Touches ONLY the two test orgs below.
#
# Requirements:
#   - CIRCLECI_CLI_TOKEN (or CIRCLE_TOKEN): a CircleCI API token whose user is an
#     ADMIN of both orgs.
#   - GITHUB_TOKEN: a GitHub PAT (repo + admin:org) for both GitHub orgs.
#   - python3, ssh-keygen, curl, jq.
#   - circleci CLI (optional): required only for orb seeding; skipped if absent.
#
# Usage:
#   CIRCLECI_CLI_TOKEN=... GITHUB_TOKEN=... scripts/dev/reset-test-orgs.sh
#
set -euo pipefail

SRC_ORG="gh-oauth-cci-1"   # source (seeded)
DST_ORG="gh-oauth-cci-2"   # destination (wiped)
REPOS=(project-1 web-app api-service)
API="https://circleci.com"
RUNNER_API="https://runner.circleci.com"

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

# Wipe dest config-policy bundle (empty bundle = no policy).
echo "    clearing dest config-policy bundle"
cci_w -X POST -H "Content-Type: application/json" \
  -d '{"policies":{}}' \
  "$API/api/v2/owner/$DST_ID/context/config/policy-bundle" >/dev/null || true

# Reset dest api-service project feature flags to defaults.
echo "    resetting dest api-service project feature flags"
cci_w -X PUT -H "Content-Type: application/json" \
  -d '{"feature_flags":{"oss":false,"build-fork-prs":false,"autocancel-builds":false}}' \
  "$API/api/v1.1/project/gh/$DST_ORG/api-service/settings" >/dev/null || true

# Wipe dest runner resource classes (namespace may not exist; guard with || true).
echo "    wiping dest runner resource classes (namespace $DST_ORG)"
runner_list="$(curl -fsS -H "Circle-Token: $TOK" \
  "$RUNNER_API/api/v3/runner/resource?namespace=$DST_ORG" 2>/dev/null || echo '{"items":[]}')"
for rid in $(printf '%s' "$runner_list" | jqr "import json,sys;[print(i['id']) for i in json.load(sys.stdin).get('items',[])]" 2>/dev/null || true); do
  curl -fsS -X DELETE -H "Circle-Token: $TOK" \
    "$RUNNER_API/api/v3/runner/resource/$rid" >/dev/null || true
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

# ── A. Org orb settings — enable on BOTH orgs ────────────────────────────────
echo "==> Enabling org orb settings on both orgs..."
ORB_FLAGS='{"feature_flags":{"allow-uncertified-public-orbs":true,"allow-private-orbs":true}}'
cci_w -X PUT -H "Content-Type: application/json" \
  -d "$ORB_FLAGS" \
  "$API/api/v1.1/organization/github/$SRC_ORG/settings" >/dev/null || true
cci_w -X PUT -H "Content-Type: application/json" \
  -d "$ORB_FLAGS" \
  "$API/api/v1.1/organization/github/$DST_ORG/settings" >/dev/null || true
echo "    orb settings enabled (allow-uncertified-public-orbs + allow-private-orbs)"

# ── B. Config policy on SRC (seed); already cleared on DST above ─────────────
echo "==> Seeding config-policy bundle on source org..."
SAMPLE_REGO='package org

policy_name["sample_migration_policy"]

enable_rule["check_version"]

check_version = reason {
  not input.version
  reason := "version must be defined"
}'
# Build the JSON body using python3 to safely embed the Rego string.
policy_body="$(python3 -c "
import json, sys
rego = open(sys.argv[1]).read()
print(json.dumps({'policies': {'sample.rego': rego}}))
" <(printf '%s' "$SAMPLE_REGO"))"
cci_w -X POST -H "Content-Type: application/json" \
  -d "$policy_body" \
  "$API/api/v2/owner/$SRC_ID/context/config/policy-bundle" >/dev/null || true
echo "    config-policy bundle seeded on $SRC_ORG (sample.rego)"

# ── C. Runner resource class on SRC ──────────────────────────────────────────
echo "==> Seeding runner resource class on source org..."
runner_http="$(curl -s -o /dev/null -w '%{http_code}' \
  -X POST -H "Circle-Token: $TOK" -H "Content-Type: application/json" \
  -d "{\"resource_class\":\"$SRC_ORG/linux-x64\",\"description\":\"migration test runner class\"}" \
  "$RUNNER_API/api/v3/runner/resource")"
case "$runner_http" in
  200|201) echo "    runner resource class $SRC_ORG/linux-x64 created" ;;
  409)     echo "    runner resource class $SRC_ORG/linux-x64 already exists — ok" ;;
  *)       echo "    WARNING: runner resource class seed got HTTP $runner_http (continuing)" ;;
esac

# ── D. Orbs on SRC — requires circleci CLI ───────────────────────────────────
echo "==> Seeding orbs on source org (requires 'circleci' CLI)..."
if ! command -v circleci >/dev/null 2>&1; then
  echo "    WARNING: 'circleci' CLI not found on PATH — skipping orb seeding."
  echo "    Install from https://circleci.com/docs/local-cli/ and re-run to seed orbs."
else
  # Write two orb YAML variants to temp files.
  ORB_V1="$(mktemp /tmp/orb-v1.XXXXXX.yml)"
  ORB_V2="$(mktemp /tmp/orb-v2.XXXXXX.yml)"
  cat >"$ORB_V1" <<'ORBEOF'
version: 2.1
description: "Migration test orb (v1)"
commands:
  greet:
    description: "Print a greeting"
    parameters:
      name:
        type: string
        default: "world"
    steps:
      - run:
          name: Greet
          command: echo "Hello, << parameters.name >>!"
ORBEOF
  cat >"$ORB_V2" <<'ORBEOF'
version: 2.1
description: "Migration test orb (v2)"
commands:
  greet:
    description: "Print a greeting"
    parameters:
      name:
        type: string
        default: "world"
    steps:
      - run:
          name: Greet
          command: echo "Hello, << parameters.name >>!"
  farewell:
    description: "Print a farewell"
    parameters:
      name:
        type: string
        default: "world"
    steps:
      - run:
          name: Farewell
          command: echo "Goodbye, << parameters.name >>!"
ORBEOF

  # Public orb: demo-orb (3 versions).
  circleci orb create "$SRC_ORG/demo-orb" --no-prompt --skip-update-check 2>&1 \
    | grep -v "already exists" || true
  circleci orb publish "$ORB_V1" "$SRC_ORG/demo-orb@0.1.0" --skip-update-check 2>&1 \
    | grep -v "already exists\|orb revision already exists" || true
  circleci orb publish "$ORB_V1" "$SRC_ORG/demo-orb@0.1.1" --skip-update-check 2>&1 \
    | grep -v "already exists\|orb revision already exists" || true
  circleci orb publish "$ORB_V2" "$SRC_ORG/demo-orb@1.0.0" --skip-update-check 2>&1 \
    | grep -v "already exists\|orb revision already exists" || true
  echo "    public orb $SRC_ORG/demo-orb seeded (versions 0.1.0, 0.1.1, 1.0.0)"

  # Private orb: demo-private (1 version).
  circleci orb create "$SRC_ORG/demo-private" --private --no-prompt --skip-update-check 2>&1 \
    | grep -v "already exists" || true
  circleci orb publish "$ORB_V1" "$SRC_ORG/demo-private@1.0.0" --skip-update-check 2>&1 \
    | grep -v "already exists\|orb revision already exists" || true
  echo "    private orb $SRC_ORG/demo-private seeded (version 1.0.0)"

  rm -f "$ORB_V1" "$ORB_V2"

  echo "    NOTE: orb versions are immutable — dest ($DST_ORG) orb state persists"
  echo "    across runs and is NOT reset by this script."
fi

# ── E. "Weird" project settings on SRC api-service ───────────────────────────
echo "==> Setting non-default project feature flags on source api-service..."
# Use individual flags; guard with || true in case a flag is rejected (returns 400).
cci_w -X PUT -H "Content-Type: application/json" \
  -d '{"feature_flags":{"oss":true,"build-fork-prs":true,"autocancel-builds":true,"build-prs-only":false}}' \
  "$API/api/v1.1/project/gh/$SRC_ORG/api-service/settings" >/dev/null || true
echo "    api-service flags set: oss=true build-fork-prs=true autocancel-builds=true build-prs-only=false"
echo "    (dest api-service was already reset to defaults in the wipe step above)"

echo
echo "==> Done. Source ($SRC_ORG) seeded; destination ($DST_ORG) wiped clean."
echo "    Seeded resources:"
echo "      - GitHub repos: ${REPOS[*]}"
echo "      - Project env vars + web-app SSH key"
echo "      - Contexts: test-1, deploy-prod, test-2-restriction"
echo "      - Org orb settings (allow-uncertified-public-orbs + allow-private-orbs)"
echo "      - Config-policy bundle (sample.rego) on source; cleared on dest"
echo "      - Runner resource class: $SRC_ORG/linux-x64 (dest classes wiped)"
echo "      - Orbs: $SRC_ORG/demo-orb (0.1.0/0.1.1/1.0.0) + demo-private (1.0.0)"
echo "        (if 'circleci' CLI was present; otherwise see WARNING above)"
echo "      - api-service non-default feature flags on source; defaults on dest"
echo ""
echo "    Run a migration, e.g.:"
echo "      export CIRCLECI_SOURCE_TOKEN=\$CIRCLECI_CLI_TOKEN CIRCLECI_DEST_TOKEN=\$CIRCLECI_CLI_TOKEN"
echo "      circleci-migrate migrate --source-org github/$SRC_ORG --dest-org github/$DST_ORG \\"
echo "        --transfer-secrets --dest-token-context migration-secrets \\"
echo "        --include-project-vars --include-ssh-keys --remove-restrictions \\"
echo "        --host-project gh/$SRC_ORG/project-1 --apply --yes"
echo "    (first store a $DST_ORG admin token in a '$SRC_ORG' context named 'migration-secrets' → CIRCLECI_DEST_TOKEN)"
echo ""
echo "    Then validate parity:"
echo "      circleci-migrate validate --source-org github/$SRC_ORG --dest-org github/$DST_ORG \\"
echo "        --mapping mapping.json --dest-orb-namespace $DST_ORG --dest-runner-namespace $DST_ORG"
