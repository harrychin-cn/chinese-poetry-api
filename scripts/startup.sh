#!/bin/sh
set -e

# Configuration
DATA_DIR="data"
DB_FILE="poetry.db"
DB_PATH="${DATA_DIR}/${DB_FILE}"
DB_GZ="${DB_PATH}.gz"
CHECKSUM_FILE="${DATA_DIR}/checksums.txt"
GITHUB_RELEASE_URL="${DATABASE_RELEASE_URL:-https://github.com/harrychin-cn/chinese-poetry-api/releases/latest/download}"
FALLBACK_RELEASE_URL="${DATABASE_FALLBACK_RELEASE_URL:-https://github.com/palemoky/chinese-poetry-api/releases/latest/download}"
ACTIVE_RELEASE_URL="${GITHUB_RELEASE_URL}"

echo "=== Chinese Poetry API Startup ==="

# Create data directory if it doesn't exist
mkdir -p "${DATA_DIR}"

# Function to download and verify database from one release URL
download_database_from_url() {
    release_url="$1"
    echo "Downloading database and checksums from ${release_url}..."

    # Download both files
    if ! curl -Lfo "${DB_GZ}" "${release_url}/${DB_FILE}.gz"; then
        echo "Warning: Failed to download database from ${release_url}"
        rm -f "${DB_GZ}"
        return 1
    fi

    if ! curl -Lfo "${CHECKSUM_FILE}" "${release_url}/checksums.txt"; then
        echo "Warning: Failed to download checksums from ${release_url}"
        rm -f "${DB_GZ}"
        return 1
    fi

    # Verify downloaded .gz file
    echo "Verifying download integrity..."
    expected_checksum=$(grep "${DB_FILE}.gz" "${CHECKSUM_FILE}" | awk '{print $1}')

    if [ -z "$expected_checksum" ]; then
        echo "ERROR: Could not find checksum for ${DB_FILE}.gz"
        rm -f "${DB_GZ}" "${CHECKSUM_FILE}"
        return 1
    fi

    actual_checksum=$(sha256sum "${DB_GZ}" | awk '{print $1}')

    if [ "$actual_checksum" != "$expected_checksum" ]; then
        echo "ERROR: Checksum mismatch!"
        echo "  Expected: $expected_checksum"
        echo "  Actual:   $actual_checksum"
        rm -f "${DB_GZ}" "${CHECKSUM_FILE}"
        return 1
    fi

    echo "OK: Download verified"

    # Extract database
    echo "Extracting ${DB_FILE}..."
    gunzip -f "${DB_GZ}"

    echo "OK: Database ready: $DB_PATH"
    return 0
}

# Function to download and verify database
download_database() {
    if download_database_from_url "${ACTIVE_RELEASE_URL}"; then
        return 0
    fi

    if [ "${ACTIVE_RELEASE_URL}" != "${FALLBACK_RELEASE_URL}" ]; then
        echo "Primary data release unavailable; trying fallback release..."
        if download_database_from_url "${FALLBACK_RELEASE_URL}"; then
            ACTIVE_RELEASE_URL="${FALLBACK_RELEASE_URL}"
            return 0
        fi
    fi

    echo "ERROR: Failed to download database from all configured release URLs"
    exit 1
}

# Function to download latest checksums from primary or fallback release
fetch_latest_checksums() {
    output_file="$1"

    if curl -Lfo "$output_file" "${GITHUB_RELEASE_URL}/checksums.txt"; then
        ACTIVE_RELEASE_URL="${GITHUB_RELEASE_URL}"
        return 0
    fi

    if [ "${FALLBACK_RELEASE_URL}" != "${GITHUB_RELEASE_URL}" ] && \
        curl -Lfo "$output_file" "${FALLBACK_RELEASE_URL}/checksums.txt"; then
        echo "Warning: Primary data release unavailable; using fallback release"
        ACTIVE_RELEASE_URL="${FALLBACK_RELEASE_URL}"
        return 0
    fi

    return 1
}

# Function to check for updates
check_for_updates() {
    echo "Checking for updates..."

    # Download latest checksums
    temp_checksum=$(mktemp)
    if ! fetch_latest_checksums "$temp_checksum"; then
        echo "Warning: Could not fetch latest checksums, skipping update check"
        rm -f "$temp_checksum"
        return 1
    fi

    # Compare with local checksums
    if [ -f "$CHECKSUM_FILE" ]; then
        if cmp -s "$temp_checksum" "$CHECKSUM_FILE"; then
            echo "OK: Database is up to date"
            rm -f "$temp_checksum"
            return 0
        else
            echo "New database version available"
            # Show what changed
            remote_checksum=$(grep "${DB_FILE}.gz" "$temp_checksum" | awk '{print $1}')
            local_checksum=$(grep "${DB_FILE}.gz" "$CHECKSUM_FILE" | awk '{print $1}')
            echo "  Local:  ${local_checksum:0:16}..."
            echo "  Remote: ${remote_checksum:0:16}..."
        fi
    fi

    rm -f "$temp_checksum"
    return 1
}

# Main logic
if [ -f "$DB_PATH" ] && [ -f "$CHECKSUM_FILE" ]; then
    echo "Database found: $DB_PATH"

    # Check for updates
    if ! check_for_updates; then
        echo "Updating database..."
        download_database
    fi
else
    echo "Database not found, downloading..."
    download_database
fi

echo "Starting API server..."
exec ./server
