#!/usr/bin/env bash
set -euo pipefail

# 校验 PrometheusRule 语法，并用单元测试确认关键告警能触发。
# 依赖：python3 + PyYAML、promtool。
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

python3 - <<'PY'
import yaml
with open("deploy/prometheus/kubedeck-rules.yaml", encoding="utf-8") as fh:
    doc = yaml.safe_load(fh)
with open("/tmp/kubedeck-rules-groups.yaml", "w", encoding="utf-8") as fh:
    yaml.safe_dump({"groups": doc["spec"]["groups"]}, fh, allow_unicode=True, sort_keys=False)
PY

promtool check rules /tmp/kubedeck-rules-groups.yaml
promtool test rules deploy/prometheus/kubedeck-rules.test.yaml