#!/usr/bin/env bash
# 本地开发数据库初始化（WSL 内以 root 执行）。
# 创建 kubedeck 角色与 kubedeck/kubedeck_test 两个库，并把连接串写入 .testenv/db.env。
# 幂等：重复执行只会重置角色密码、保留已有数据。
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ENV_FILE="$ROOT/.testenv/db.env"
DB_USER="kubedeck"
DB_PASS=""

psql_su() {
  su postgres -c "psql -v ON_ERROR_STOP=1 $*"
}

psql_scalar() {
  su postgres -c "psql -tAc \"$1\""
}

mkdir -p "$ROOT/.testenv"

if [ -f "$ENV_FILE" ]; then
  DB_PASS="$(sed -n 's#^KUBEDECK_DATABASE_URL=postgres://[^:]*:\([^@]*\)@.*#\1#p' "$ENV_FILE" | head -1)"
fi
if [ -z "$DB_PASS" ]; then
  DB_PASS="$(openssl rand -hex 16)"
fi

if [ "$(psql_scalar "select 1 from pg_roles where rolname='$DB_USER'")" = "1" ]; then
  psql_su "-c \"ALTER ROLE $DB_USER PASSWORD '$DB_PASS'\"" >/dev/null
  echo "role $DB_USER exists, password reset"
else
  psql_su "-c \"CREATE ROLE $DB_USER LOGIN PASSWORD '$DB_PASS'\"" >/dev/null
  echo "role $DB_USER created"
fi

for db in kubedeck kubedeck_test; do
  if [ "$(psql_scalar "select 1 from pg_database where datname='$db'")" = "1" ]; then
    echo "database $db exists"
  else
    psql_su "-c \"CREATE DATABASE $db OWNER $DB_USER\"" >/dev/null
    echo "database $db created"
  fi
done

umask 077
cat > "$ENV_FILE" <<EOF
KUBEDECK_DATABASE_URL=postgres://$DB_USER:$DB_PASS@127.0.0.1:5432/kubedeck?sslmode=disable
KUBEDECK_TEST_DATABASE_URL=postgres://$DB_USER:$DB_PASS@127.0.0.1:5432/kubedeck_test?sslmode=disable
EOF
chmod 600 "$ENV_FILE"
echo "wrote $ENV_FILE (gitignored)"