#!/usr/bin/env bash
# Testa se o endpoint de ranking do SF6 aceita variantes de page size.
# Uso:  ./scripts/test-ranking-pagesize.sh
# Requer: .env com SF6_COOKIE definido.

set -euo pipefail

cd "$(dirname "$0")/.."

# Carrega cookie do .env
if [[ ! -f .env ]]; then
	echo "❌ .env não encontrado"
	exit 1
fi
# shellcheck disable=SC1091
source .env
if [[ -z "${SF6_COOKIE:-}" ]]; then
	echo "❌ SF6_COOKIE vazio no .env"
	exit 1
fi

# 1) Pega o buildID atual do SF6 (mesma lógica do buildid.go)
echo "→ Buscando buildID atual do SF6..."
BUILD_ID=$(curl -s "https://www.streetfighter.com/6/buckler/en/profile/0/battlelog" \
	-H "user-agent: Mozilla/5.0" \
	| grep -oE '"buildId"\s*:\s*"[^"]+"' \
	| head -1 \
	| sed -E 's/.*"buildId"\s*:\s*"([^"]+)".*/\1/')

if [[ -z "$BUILD_ID" ]]; then
	echo "❌ Não consegui extrair buildID"
	exit 1
fi
echo "✅ buildID: $BUILD_ID"
echo ""

BASE="https://www.streetfighter.com/6/buckler/_next/data/${BUILD_ID}/pt-br/ranking/league.json"

# Função: faz a request, conta entries em ranking_fighter_list e mostra o tamanho
test_variant() {
	local label="$1"
	local url="$2"

	echo "─── $label ───"
	echo "URL: $url"
	local resp
	resp=$(curl -s "$url" \
		-H "Cookie: $SF6_COOKIE" \
		-H "user-agent: Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/147.0.0.0 Safari/537.36" \
		-H "x-nextjs-data: 1" \
		-H "accept: */*")

	local count
	count=$(echo "$resp" | jq -r '.pageProps.league_point_ranking.ranking_fighter_list | length' 2>/dev/null || echo "?")
	local total_page
	total_page=$(echo "$resp" | jq -r '.pageProps.league_point_ranking.total_page' 2>/dev/null || echo "?")
	local size_bytes
	size_bytes=$(echo -n "$resp" | wc -c | tr -d ' ')

	echo "  entries:    $count"
	echo "  total_page: $total_page"
	echo "  bytes:      $size_bytes"
	echo ""
}

# Variantes a testar
test_variant "Baseline (sem param)"      "${BASE}?page=2"
test_variant "?per_page=100"             "${BASE}?page=2&per_page=100"
test_variant "?limit=100"                "${BASE}?page=2&limit=100"
test_variant "?page_size=100"            "${BASE}?page=2&page_size=100"
test_variant "?size=100"                 "${BASE}?page=2&size=100"
test_variant "?count=100"                "${BASE}?page=2&count=100"
test_variant "?pageSize=100"             "${BASE}?page=2&pageSize=100"

echo "→ Se algum mostrar 'entries: 100' (ou mais que 20), encontramos o param!"
echo "→ Se todos mostrarem 'entries: 20', o tamanho é fixo no SF6."
