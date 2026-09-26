#!/bin/bash
# 4 versioni di reap reinvest x 3 giri sugli stessi dati: tempo, stabilità, proposte
cd "$(dirname "$0")"
declare -a V=(
  "attuale|"
  "solo-skillssh|--catalogo skillssh"
  "solo-verifica|--selezione verifica"
  "gruppi-haiku|--gruppi-modello haiku"
)
for giro in 1 2 3; do
  for v in "${V[@]}"; do
    name="${v%%|*}"; opts="${v#*|}"
    echo "=== $name giro $giro"
    .venv/bin/python -u reinvest_prova.py $opts --tag "_${name}_$giro" > "prove_laya/log_${name}_$giro.txt" 2>&1
    grep -E "Verifica:|Tempo:" "prove_laya/log_${name}_$giro.txt"
  done
done
