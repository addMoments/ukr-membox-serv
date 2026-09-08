#!/bin/bash
# Mevcut uploads satirlarina S3'teki gercek dosya boyutunu yazar (Excel madde 2.17).
#
# Neden gerekli: size_bytes kolonu 0 varsayilaniyla eklendi. Depolama limiti bu kolonun
# toplamina bakiyor; eski yuklemeler 0 kalirsa dolu bir etkinlik bos gorunur.
#
# Nasil: S3'teki events/ onekindeki tum objelerin (key, size) listesi alinir, gecici bir
# tabloya yazilir ve uploads.value ile eslesenler guncellenir. Yalnizca size_bytes = 0 olan
# satirlara dokunur, yani tekrar tekrar calistirilabilir.
#
# Kullanim:
#   PGPASSWORD=... AWS_PROFILE=addmoments bash backfill-upload-sizes.sh <db-host> <db-user> <db-name>

set -euo pipefail

DB_HOST="${1:?db host required}"
DB_USER="${2:?db user required}"
DB_NAME="${3:?db name required}"
BUCKET="${BUCKET:-memboxpub-qo1gff2e}"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

echo "S3 listeleniyor: s3://$BUCKET/events/"
aws s3api list-objects-v2 --bucket "$BUCKET" --prefix "events/" \
  --output json --query 'Contents[].[Key,Size]' > "$TMP/objects.json"

python3 - "$TMP/objects.json" "$TMP/sizes.csv" <<'PY'
import csv, json, sys
rows = json.load(open(sys.argv[1])) or []
with open(sys.argv[2], 'w', newline='') as f:
    w = csv.writer(f)
    for key, size in rows:
        # uploads.value bas taraftaki "/" ile saklaniyor.
        w.writerow(['/' + key, size])
print(f"{len(rows)} obje listelendi", file=sys.stderr)
PY

psql -h "$DB_HOST" -U "$DB_USER" -d "$DB_NAME" -v ON_ERROR_STOP=1 <<SQL
BEGIN;
CREATE TEMP TABLE s3_sizes (path TEXT PRIMARY KEY, size_bytes BIGINT);
\copy s3_sizes FROM '$TMP/sizes.csv' CSV
UPDATE uploads u
SET size_bytes = s.size_bytes
FROM s3_sizes s
WHERE s.path = u.value
  AND u.size_bytes = 0
  AND s.size_bytes > 0;
SELECT
  (SELECT COUNT(*) FROM uploads WHERE upload_type IN ('photo','video','voice')) AS media_rows,
  (SELECT COUNT(*) FROM uploads WHERE upload_type IN ('photo','video','voice') AND size_bytes > 0) AS rows_with_size,
  pg_size_pretty((SELECT COALESCE(SUM(size_bytes),0) FROM uploads)) AS total_size;
COMMIT;
SQL
