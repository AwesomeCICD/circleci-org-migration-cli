#!/bin/sh
# Merge per-context/project secret bundles into a single bundle.
# Optionally encrypts the output with age and/or uploads to S3.
#
# Parameters are passed as environment variables by the orb job:
#   ORB_PARAM_ENCRYPT_RECIPIENT  — age/SSH public key string; if non-empty,
#                                  encrypt the merged bundle (writes
#                                  secrets.json.age instead of plaintext).
#   ORB_PARAM_S3_BUCKET          — S3 bucket name; if non-empty, upload the
#                                  bundle (encrypted if recipient is set) to
#                                  s3://<bucket>/<prefix><filename>.
#   ORB_PARAM_S3_PREFIX          — S3 key prefix (optional, e.g. "migration/").
#
# SECURITY: ORB_PARAM_ENCRYPT_RECIPIENT is a PUBLIC key — safe to embed in
# config and environment variables. Private keys are NEVER used here.

set -e

encrypt_recipient="${ORB_PARAM_ENCRYPT_RECIPIENT:-}"
s3_bucket="${ORB_PARAM_S3_BUCKET:-}"
s3_prefix="${ORB_PARAM_S3_PREFIX:-}"

# ── Merge bundles ─────────────────────────────────────────────────────────────

if [ -n "$encrypt_recipient" ]; then
  # Merge to plaintext first, then encrypt.
  circleci-migrate secrets merge -o secrets.json captured/*.json

  # Encrypt the merged bundle.
  circleci-migrate bundle-encrypt \
    --recipient "$encrypt_recipient" \
    --input secrets.json \
    --output secrets.json.age

  # Remove the plaintext bundle so it is never stored as an artifact.
  rm -f secrets.json

  bundle_file="secrets.json.age"
else
  circleci-migrate secrets merge -o secrets.json captured/*.json
  bundle_file="secrets.json"
fi

# ── S3 upload (optional) ──────────────────────────────────────────────────────

if [ -n "$s3_bucket" ]; then
  s3_key="${s3_prefix}${bundle_file}"
  s3_url="s3://${s3_bucket}/${s3_key}"
  echo "Uploading ${bundle_file} to ${s3_url} ..."
  aws s3 cp "$bundle_file" "$s3_url"
  echo "Uploaded to ${s3_url}"
fi
