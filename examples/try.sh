#!/usr/bin/env bash
# Проверяет шаблон и выпускает по нему документ в DOCX и PDF.
#
#   examples/try.sh examples/opening                  # все data*.json из папки
#   examples/try.sh examples/opening data-edge.json   # один набор
#   examples/try.sh путь/к/шаблону.docx данные.json   # свой шаблон
#
# Нужны DOCGEN_KEY (ключ портала из make keygen) и запущенный сервис;
# адрес — DOCGEN_URL (по умолчанию http://localhost:8080).
# Результаты — в examples/out/<шаблон>/: файлы документов и ответы с ошибками.
set -euo pipefail

: "${DOCGEN_KEY:?задайте DOCGEN_KEY=dg_... (make keygen)}"
URL=${DOCGEN_URL:-http://localhost:8080}
HERE=$(cd "$(dirname "$0")" && pwd)

if [[ -d ${1:?укажите папку примера или .docx} ]]; then
  TEMPLATE=$1/template.docx
  NAME=$(basename "$1")
  if [[ -n ${2:-} ]]; then DATA=("$1/$2"); else DATA=("$1"/data*.json); fi
else
  TEMPLATE=$1
  NAME=$(basename "$1" .docx)
  DATA=("${2:?укажите файл данных}")
fi
OUT=$HERE/out/$NAME
mkdir -p "$OUT"

echo "== Проверка шаблона $TEMPLATE"
code=$(curl -s -o "$OUT/check.json" -w '%{http_code}' "$URL/v1/templates/check" \
  -H "X-API-Key: $DOCGEN_KEY" -F "template=@$TEMPLATE")
if [[ $code != 200 ]]; then
  echo "   $code — шаблон не принят:"; jq -r '.error.details[]? | "   • \(.part) «\(.context)»: \(.message)"' "$OUT/check.json"
  exit 1
fi
echo "   ок, полей: $(jq '.variables | length' "$OUT/check.json") (список — $OUT/check.json)"

for data in "${DATA[@]}"; do
  set_name=$(basename "$data" .json)
  for format in docx pdf; do
    file=$OUT/$set_name.$format
    # Новый ключ идемпотентности на каждый запуск: иначе вернётся документ прошлого запуска.
    code=$(curl -s -D "$OUT/headers" -o "$file" -w '%{http_code}' "$URL/v1/documents" \
      -H "X-API-Key: $DOCGEN_KEY" -H "Idempotency-Key: try-$NAME-$set_name-$format-$(date +%s%N)" \
      -F "template=@$TEMPLATE" -F "data=@$data" -F format=$format \
      -F "template_name=$NAME" -F "external_id=try:$set_name" -F requested_by=tester -F retention_days=1)
    if [[ $code == 201 ]]; then
      printf '   %-24s %s  %s\n' "$set_name.$format" "$code" "$file"
    else
      mv "$file" "$OUT/$set_name.$format.error.json"
      printf '   %-24s %s  %s\n' "$set_name.$format" "$code" "$(jq -r '.error.code' "$OUT/$set_name.$format.error.json")"
      [[ $format == docx ]] && jq -r '.error.details[]? | "      • \(.field // .context): \(.message)"' "$OUT/$set_name.$format.error.json"
    fi
  done
done
