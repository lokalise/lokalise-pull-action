#!/bin/bash

commit_and_push_changes() {
    git config --global user.name "${GITHUB_ACTOR}"
    git config --global user.email "${GITHUB_ACTOR}@users.noreply.github.com"

    # Generate branch name
    TIMESTAMP=$(date +%s)
    SHORT_SHA=${GITHUB_SHA::6}
    SAFE_REF_NAME=$(echo "${GITHUB_REF_NAME}" | tr -cd '[:alnum:]_-' | cut -c1-50)
    BRANCH_NAME="${TEMP_BRANCH_PREFIX}_${SAFE_REF_NAME}_${SHORT_SHA}_${TIMESTAMP}"
    BRANCH_NAME=$(echo "$BRANCH_NAME" | tr -cd '[:alnum:]_-' | cut -c1-255)
    echo "branch_name=$BRANCH_NAME" >> $GITHUB_ENV

    git checkout -b "$BRANCH_NAME" || git checkout "$BRANCH_NAME"

    # Set up paths and file formats from env variables
    paths=()
    while IFS= read -r path; do
      path=$(echo "$path" | xargs)  # Trim whitespace
      if [ -n "$path" ]; then
        paths+=("$path")
      fi
    done <<< "$TRANSLATIONS_PATH"

    add_args=()
    for path in "${paths[@]}"; do
      if [[ "$FLAT_NAMING" == "true" ]]; then
        # Flat structure: add only top-level files, exclude subdirectories
        if [[ "$ALWAYS_PULL_BASE" == "true" ]]; then
          add_args+=("$path/*.${FILE_FORMAT}")
        else
          add_args+=("$path/*.${FILE_FORMAT}")
          add_args+=(":!$path/$BASE_LANG.${FILE_FORMAT}")
        fi
        # Exclude all files in subdirectories when flat_naming is true
        add_args+=(":!$path/**/*.${FILE_FORMAT}")
      else
        # Folder structure: include all files in language-named subdirectories
        if [[ "$ALWAYS_PULL_BASE" == "true" ]]; then
          add_args+=("$path/**/*.${FILE_FORMAT}")
        else
          add_args+=("$path/**/*.${FILE_FORMAT}")
          add_args+=(":!$path/$BASE_LANG/**")
        fi
      fi
    done

    git add "${add_args[@]}" --force

    if git commit -m 'Translations update'; then
      git push origin "$BRANCH_NAME"
      return 0
    else
      echo "No changes to commit"
      return 1
    fi
}
