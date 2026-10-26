#!/usr/bin/env bash
set -euo pipefail

# Generate ClickHouse migrations ConfigMap from migrations/ directory
# This script is the bridge between migrations/ (source of truth) and
# config/components/clickhouse-migrations/configmap.yaml (Kubernetes)

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
MIGRATIONS_DIR="${REPO_ROOT}/migrations"
OUTPUT_FILE="${REPO_ROOT}/config/components/clickhouse-migrations/configmap.yaml"
ENVIRONMENTS_DIR="${MIGRATIONS_DIR}/environments"
OVERLAYS_DIR="${REPO_ROOT}/config/overlays"

# Shared migrations use versions 001-899. Versions 900-999 belong to migrations
# under migrations/environments/<env>/, which only that environment applies.
check_versions() {
    local dir="$1" min="$2" max="$3" file name version seen=" "
    for file in $(find "${dir}" -maxdepth 1 -name "*.sql" -type f | sort); do
        name=$(basename "${file}")
        if [[ ! "${name}" =~ ^([0-9]{3})_.+\.sql$ ]]; then
            echo "ERROR: ${name} does not match {version}_{name}.sql"
            exit 1
        fi
        version=$((10#${BASH_REMATCH[1]}))
        if [ "${version}" -lt "${min}" ] || [ "${version}" -gt "${max}" ]; then
            echo "ERROR: ${dir#${REPO_ROOT}/}/${name} must use a version from ${min} to ${max}"
            exit 1
        fi
        if [[ "${seen}" == *" ${version} "* ]]; then
            echo "ERROR: ${dir#${REPO_ROOT}/}/${name} reuses version ${version}; migrate.sh would skip it as applied"
            exit 1
        fi
        seen="${seen}${version} "
    done
}

check_versions "${MIGRATIONS_DIR}" 1 899
if [ -d "${ENVIRONMENTS_DIR}" ]; then
    for env_dir in "${ENVIRONMENTS_DIR}"/*/; do
        [ -d "${env_dir}" ] || continue
        check_versions "${env_dir%/}" 900 999
    done
fi

echo "Generating ClickHouse migrations ConfigMap..."
echo "Source: ${MIGRATIONS_DIR}"
echo "Output: ${OUTPUT_FILE}"

# Start ConfigMap
cat > "${OUTPUT_FILE}" << 'EOF'
# AUTO-GENERATED - DO NOT EDIT MANUALLY
# Generated from migrations/ directory (source of truth)
# To regenerate: task migrations:generate OR ./hack/generate-migrations-configmap.sh
#
# To add a new migration:
# 1. Create file in migrations/ (e.g., migrations/002_add_field.sql)
# 2. Run: task migrations:generate
# 3. Update job.yaml to include the new migration in volumes
# 4. Deploy: task dev:deploy

apiVersion: v1
kind: ConfigMap
metadata:
  name: clickhouse-migrations
  namespace: activity-system
  labels:
    app: clickhouse-migrations
    app.kubernetes.io/component: database
data:
  # Migration runner script
  migrate.sh: |
EOF

# Add migrate.sh with proper indentation (skip whitespace-only lines)
if [ -f "${MIGRATIONS_DIR}/migrate.sh" ]; then
    while IFS= read -r line || [ -n "${line}" ]; do
        if [ -z "${line// /}" ]; then
            # Line is empty or only whitespace - output just newline
            echo "" >> "${OUTPUT_FILE}"
        else
            # Line has content - indent it
            echo "    ${line}" >> "${OUTPUT_FILE}"
        fi
    done < "${MIGRATIONS_DIR}/migrate.sh"
else
    echo "ERROR: ${MIGRATIONS_DIR}/migrate.sh not found"
    exit 1
fi

echo "" >> "${OUTPUT_FILE}"

# Add all SQL migration files
sql_files=$(find "${MIGRATIONS_DIR}" -maxdepth 1 -name "*.sql" -type f | sort)

if [ -z "${sql_files}" ]; then
    echo "WARNING: No SQL migration files found in ${MIGRATIONS_DIR}"
fi

for migration in ${sql_files}; do
    filename=$(basename "${migration}")
    echo "  # Migration: ${filename}"
    echo "  ${filename}: |" >> "${OUTPUT_FILE}"

    # Add SQL file with proper indentation (skip whitespace-only lines)
    while IFS= read -r line || [ -n "${line}" ]; do
        if [ -z "${line// /}" ]; then
            # Line is empty or only whitespace - output just newline
            echo "" >> "${OUTPUT_FILE}"
        else
            # Line has content - indent it
            echo "    ${line}" >> "${OUTPUT_FILE}"
        fi
    done < "${migration}"

    echo "" >> "${OUTPUT_FILE}"
done

echo "✅ Generated ${OUTPUT_FILE}"
echo ""
echo "Migration files included:"
echo "${sql_files}" | sed 's|.*/|  - |'
echo ""
echo "Next steps:"
echo "1. Update job.yaml if you added new migrations"
echo "2. Deploy: task dev:deploy"

# Each migrations/environments/<env>/ becomes config/overlays/clickhouse-migrations-<env>/:
# the shared component plus a patch that adds that environment's files to the same
# ConfigMap, so the Job applies them after the shared ones.
if [ -d "${ENVIRONMENTS_DIR}" ]; then
    for env_dir in "${ENVIRONMENTS_DIR}"/*/; do
        [ -d "${env_dir}" ] || continue
        env_name=$(basename "${env_dir}")
        env_sql_files=$(find "${env_dir}" -maxdepth 1 -name "*.sql" -type f | sort)
        if [ -z "${env_sql_files}" ]; then
            echo "ERROR: ${env_dir} holds no migrations"
            exit 1
        fi

        env_out="${OVERLAYS_DIR}/clickhouse-migrations-${env_name}"
        mkdir -p "${env_out}"

        cat > "${env_out}/kustomization.yaml" << EOF
# AUTO-GENERATED - DO NOT EDIT MANUALLY
# Generated from migrations/environments/${env_name}/ by hack/generate-migrations-configmap.sh
#
# The shared migrations plus the ones only ${env_name} applies. Point the
# environment's migrations deployment at this directory instead of
# components/clickhouse-migrations.

apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization

components:
  - ../../components/clickhouse-migrations

patches:
  - path: configmap-patch.yaml
EOF

        env_patch="${env_out}/configmap-patch.yaml"
        cat > "${env_patch}" << EOF
# AUTO-GENERATED - DO NOT EDIT MANUALLY
# Generated from migrations/environments/${env_name}/ by hack/generate-migrations-configmap.sh

apiVersion: v1
kind: ConfigMap
metadata:
  name: clickhouse-migrations
  namespace: activity-system
data:
EOF

        for migration in ${env_sql_files}; do
            filename=$(basename "${migration}")
            echo "  # ${env_name} migration: ${filename}"
            echo "  ${filename}: |" >> "${env_patch}"
            while IFS= read -r line || [ -n "${line}" ]; do
                if [ -z "${line// /}" ]; then
                    echo "" >> "${env_patch}"
                else
                    echo "    ${line}" >> "${env_patch}"
                fi
            done < "${migration}"
            echo "" >> "${env_patch}"
        done

        echo "✅ Generated ${env_out#${REPO_ROOT}/}"
    done
fi

# An environment whose migrations directory is gone loses its generated overlay too,
# so the bundle never publishes a migration set nobody maintains.
for env_out in "${OVERLAYS_DIR}"/clickhouse-migrations-*/; do
    [ -d "${env_out}" ] || continue
    env_name=$(basename "${env_out}")
    env_name="${env_name#clickhouse-migrations-}"
    [ -d "${ENVIRONMENTS_DIR}/${env_name}" ] && continue
    if [ "$(head -n 1 "${env_out}kustomization.yaml" 2>/dev/null)" != "# AUTO-GENERATED - DO NOT EDIT MANUALLY" ]; then
        echo "ERROR: ${env_out#${REPO_ROOT}/} has no migrations/environments/${env_name}/ and is not generated; remove it by hand"
        exit 1
    fi
    rm -rf "${env_out}"
    echo "🗑  Removed ${env_out#${REPO_ROOT}/}: migrations/environments/${env_name}/ no longer exists"
done
