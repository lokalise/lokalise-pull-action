#!/bin/bash

return_with_error() {
    echo "Error: $1" >&2
    return 1
}

download_files() {
    local project_id=$1
    local token=$2
    local additional_params="${CLI_ADD_PARAMS:-}"
    local attempt=0
    local max_retries="${MAX_RETRIES:-5}"
    local sleep_time="${SLEEP_TIME:-1}"
    local max_sleep_time=60
    local max_total_time=300
    local start_time=$(date +%s)
    local file_format="${FILE_FORMAT}"
    local github_ref_name="${GITHUB_REF_NAME}"

    [[ -z "$project_id" ]] && return_with_error "project_id is required and cannot be empty."
    [[ -z "$token" ]] && return_with_error "token is required and cannot be empty."

    if [[ "$sleep_time" -lt 1 ]]; then
        sleep_time=1
    elif [[ "$sleep_time" -gt "$max_sleep_time" ]]; then
        sleep_time=$max_sleep_time
    fi

    if ! [[ "$max_retries" =~ ^[0-9]+$ ]] || [[ "$max_retries" -lt 1 ]]; then
        max_retries=5
    fi

    if ! [[ "$max_total_time" =~ ^[0-9]+$ ]] || [[ "$max_total_time" -lt $max_sleep_time ]]; then
        max_total_time=300
    fi

    echo "Starting download for project: $project_id"
    while [ $attempt -lt $max_retries ]; do
        echo "Attempt $((attempt + 1)) of $max_retries"

        set +e

        output=$(./bin/lokalise2 --token="$token" \
            --project-id="$project_id" \
            file download \
            --format="$file_format" \
            --original-filenames=true \
            --directory-prefix="/" \
            --include-tags="$github_ref_name" \
            $additional_params 2>&1)

        exit_code=$?

        set -e

        if [ $exit_code -eq 0 ]; then
            echo "Successfully downloaded files"
            return 0
        elif echo "$output" | grep -q 'API request error 429'; then
            attempt=$((attempt + 1))
            current_time=$(date +%s)
            elapsed_time=$((current_time - start_time))
            if [ $elapsed_time -ge $max_total_time ]; then
                return_with_error "Max retry time exceeded before sleeping. Exiting."
            fi
            echo "Attempt $attempt failed with API request error 429. Retrying in $sleep_time seconds..."
            sleep $sleep_time
            sleep_time=$((sleep_time * 2))
            if [ $sleep_time -gt $max_sleep_time ]; then
                sleep_time=$max_sleep_time
            fi
        elif echo "$output" | grep -q 'API request error 406'; then
            echo "API request error 406: No keys for export with current export settings. Exiting..."
            return 0
        else
            return_with_error "Error encountered during download: $output"
        fi
    done

    return_with_error "Failed to download files after $max_retries attempts"
}