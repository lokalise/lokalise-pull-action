# GitHub action to pull translation files from Lokalise

![GitHub Release](https://img.shields.io/github/v/release/lokalise/lokalise-pull-action)

GitHub action to download translation files from [Lokalise TMS](https://lokalise.com/) to your GitHub repository in the form of a pull request.

* Step-by-step tutorial covering the usage of this action is available on [Lokalise Developer Hub](https://developers.lokalise.com/docs/github-actions)
* If you're looking for an in-depth tutorial, [check out our blog post](https://lokalise.com/blog/github-actions-for-lokalise-translation/)

To upload translation files from GitHub to Lokalise, use the [lokalise-push-action](https://github.com/lokalise/lokalise-push-action).

*To find documentation for the **stable version 3**, [browse the v3 tag](https://github.com/lokalise/lokalise-pull-action/tree/v3).*

## Usage

Use this action in the following way:

```yaml
name: Demo pull with tags

on:
  workflow_dispatch:

jobs:
  build:
    runs-on: ubuntu-latest

    steps:
      - name: Checkout Repo
        uses: actions/checkout@v5
        with:
          fetch-depth: 0

      - name: Pull from Lokalise
        uses: lokalise/lokalise-pull-action@v4.2.0
        with:
          api_token: ${{ secrets.LOKALISE_API_TOKEN }}
          project_id: LOKALISE_PROJECT_ID
          base_lang: en
          translations_path: |
            TRANSLATIONS_PATH1
            TRANSLATIONS_PATH2
          file_format: json
          additional_params: >
            {
              "indentation": "2sp",
              "export_empty_as": "skip",
              "export_sort": "a_z",
              "replace_breaks": false,
              "language_mapping": [
                {"original_language_iso": "en_US", "custom_language_iso": "en-US"}
              ]
            }
```

### Important note on Lokalise filenames and tags

Before running this action, ensure that your translation keys in Lokalise are properly assigned with relevant [filenames](https://docs.lokalise.com/en/articles/2281317-filenames) and [tags](https://docs.lokalise.com/en/articles/1475552-tags).

#### Tags and branch context

By default, the action filters downloaded keys based on a tag that matches the Git branch name used to trigger the workflow. This is done automatically via the `"include_tags": [BRANCH_NAME]` API parameter.

For example, if the action is triggered from the `hub` branch on GitHub, it will download only the keys tagged with `hub`: `"include_tags": ["hub"]`. If no keys with the specified tag are found, the action will terminate.

To disable this automatic filtering or use custom tags, set the `skip_include_tags` parameter to `true`. You can also provide your own tags via the `additional_params` field:

```yaml
skip_include_tags: true
additional_params: >
  {
    "include_tags": ["release-2025-08-19"]
  }
```

#### Filenames and directory structure

If you specify `locales` as the `translations_path`, your keys must include filenames that align with this structure, such as:

- `locales/%LANG_ISO%.json`
- `locales/%LANG_ISO%/main.xml`

Here:

- `%LANG_ISO%` will be replaced with the language code (e.g., `en`, `fr`, etc.).

If the filenames do not include the correct directory prefix (like `locales/`), the action will fail to compare the downloaded files with the existing files in your `translations_path`. In this case, the workflow logs will show the message: "No changes detected in translation files.".

To avoid this, double-check that your Lokalise filenames match the expected directory structure.

Note that by default the action adds the `"original_filenames": true` and `"directory_prefix": "/"` API parameters. To disable this behavior, set the `skip_original_filenames` to `true`.

## Configuration

You'll need to provide some parameters for the action. These can be set as environment variables, secrets, or passed directly. Refer to the [General setup](https://developers.lokalise.com/docs/github-actions#general-setup-overview) section for detailed instructions.

### Mandatory parameters

- `api_token` — Lokalise API token with read/write permissions.
- `project_id` — Your Lokalise project ID.
- `translations_path` — One or more paths to your translation files. Do not provide the actual filenames here. For example, if your translations are stored in the `locales` folder at the project root, use `locales`. Defaults to `locales`.
- `file_format` — Defines the format of your translation files, such as `json` for JSON files. Defaults to `json`. This format determines how translation files are processed and also influences the file extension used when searching for them. However, some specific formats, such as `json_structured`, may still be downloaded with a generic `.json` extension. If you're using such a format, make sure to set the `file_ext` parameter explicitly to match the correct extension for your files.
- `base_lang` — Your project base language, such as `en` for English. Defaults to `en`.
- `file_ext` (*not strictly mandatory but recommended*) — One or more custom file extensions to use when searching for translation files (without leading dot, e.g. `json` or `yml`). By default, the extension is inferred from the `file_format` value. However, for certain formats (e.g. `json_structured`) or mixed bundles (e.g. iOS, which uses both `.strings` and `.stringsdict`), the downloaded files may still have a generic extension or require multiple extensions. In such cases, this parameter allows specifying the correct extension(s) manually to ensure proper file matching.

```yaml
file_ext: json

# Or (useful when the bundle contains miltiple extensions)

file_ext: |
  strings
  stringsdict
```

### Download options

- `async_mode` — Download translations in asynchronous mode. Not recommended for small projects but required for larger ones (>= 10 000 key-language pairs). Defaults to `false`.
- `flat_naming` — Use flat naming convention. Set to `true` if your translation files follow a flat naming pattern like `locales/en.json` instead of `locales/en/file.json`. Defaults to `false`.
- `skip_include_tags` — Skip setting the `"include_tags"` param during download. This will download all translation keys for the specified format, regardless of tags.
- `skip_original_filenames` — Skips setting the `"original_filenames": true` and `"directory_prefix": "/"` params during download. You can disable original filenames by setting `"original_filenames": false` explicitly via `additional_params`.
- `additional_params` — Extra parameters to pass when sending [File download API request](https://developers.lokalise.com/reference/download-files). Must be valid JSON. For example, you can use `"indentation": "2sp"` to manage indentation. Defaults to an empty string. Multiple params can be specified:

```yaml
additional_params: >
  {
    "indentation": "2sp",
    "export_empty_as": "skip",
    "export_sort": "a_z",
    "replace_breaks": false,
    "include_tags": ["release-2025-08-19"],
    "language_mapping": [
      {"original_language_iso": "en_US", "custom_language_iso": "en-US"}
    ]
  }
```

### Post-processing

- `post_process_command` — A shell command that runs after pulling translation files from Lokalise but before committing them. This allows you to perform custom transformations, cleanup, replacements, or validations on the downloaded files. The command is executed in the root of your repository and has access to several environment variables (`TRANSLATIONS_PATH`, `BASE_LANG`, `FILE_FORMAT`, `FILE_EXT`, `FLAT_NAMING`, `PLATFORM`).
  + Please note that this is an **experimental feature**. You are fully responsible for the logic and behavior of any script executed through this option. These scripts run in your own repository context, under your control. If something breaks or behaves unexpectedly, we cannot guarantee support or ensure the security of the code being executed.
  + This is executed inside a Bash shell (`shell: bash`) therefore your command must be runnable from Bash. If you need a different interpreter or shell, call it explicitly, for example `post_process_command: "zsh -c 'source ~/.zshrc && run_my_script'"`.
  + If your command requires a custom interpreter (e.g. running tools that are not available by default on GitHub-hosted runners), you are responsible for setting it up yourself before the command is executed.

```yaml
# This will run replace_test.py file from the scripts folder in the root of your repo
post_process_command: "python scripts/replace_test.py"

# Or using a simple shell one-liner:
post_process_command: "sed -i 's/test/REPLACED/g' messages/fr.json"

# You can also run custom tools or binaries:
post_process_command: "./scripts/postprocess"
```

Then, for example, code post-processing logic inside the `./scripts/replace_test.py` file:

```py
def replace_values(obj):
    if isinstance(obj, dict):
        return {k: replace_values(v) for k, v in obj.items()}
    elif isinstance(obj, list):
        return [replace_values(v) for v in obj]
    elif isinstance(obj, str):
        return obj.replace("test", "REPLACED")
    else:
        return obj

# TRANSLATIONS_PATH, FILE_EXT are set for you
translations_path = os.getenv("TRANSLATIONS_PATH", "locales")
file_ext = os.getenv("FILE_EXT", "json")
file_path = os.path.join(translations_path, f"fr.{file_ext}")

with open(file_path, "r", encoding="utf-8") as f:
    data = json.load(f)

with open(file_path, "w", encoding="utf-8") as f:
    json.dump(replace_values(data), f, ensure_ascii=False, indent=2)
```

- `post_process_strict` — Whether to fail the workflow if the `post_process_command` fails (non-zero exit code). If set to `true`, the workflow will exit immediately on failure. Defaults to `false`.

### Update behavior

- `always_pull_base` — By default, changes in the base language translation files (defined by the `base_lang` option) are ignored when checking for updates. Set this option to `true` to include changes in the base language translations in the pull request. Defaults to `false`.

### Retry and timeout

- `max_retries` — Maximum number of retries on rate limit (HTTP 429) and other retryable errors. Defaults to `3`.
- `sleep_on_retry` — Number of seconds to sleep before retrying on retryable errors (exponential backoff applies). Defaults to `1`.
- `http_timeout` — Timeout in seconds for every HTTP operation (requesting bundle, downloading archive, etc.). Defaults to `120`.
- `async_poll_initial_wait` — Number of seconds to wait before polling the async download process for the first time. Has no effect if the `async_mode` is disabled. Defaults to `1`.
- `async_poll_max_wait` — Timeout for polling the async download process. Has no effect if the `async_mode` is disabled. Defaults to `120`.
- `download_timeout` — Timeout in seconds for the whole download and unzip operation. Defaults to `600`.

### Git identity

- `git_user_name` — Optional Git username for commits. Defaults to the GitHub actor of the workflow run. Handy for using a specific identity (e.g., "Localization Bot").
- `git_user_email` — Optional Git email for commits. Defaults to a noreply address based on the username (e.g., `username@users.noreply.github.com`). Useful for cleaner commit metadata or bot identities.

### Commit and branch control

- `git_commit_message` — Custom commit message. Defaults to "Translations update".
- `override_branch_name` — Static branch name instead of an auto-generated one. Helps update the same PR across runs (e.g., always `lokalise-sync`). If the branch exists, it’s updated rather than recreated.
- `force_push` — Force push to the remote branch. Use with caution, as it overwrites history. Defaults to `false`.
- `temp_branch_prefix` — Prefix for temporary branch names (e.g., `lok` — branch starts with `lok`). Defaults to `lok`.

### Pull request details

- `pr_title` — Title for the PR. Defaults to "Translations update".
- `pr_body` — Body text for the PR. Defaults to "This pull request updates translations from Lokalise".
- `pr_labels` — Comma-separated labels to apply to the PR.
- `pr_draft` — Create the PR as a draft (`true`/`false`). Defaults to `false`.
- `pr_assignees` — Comma-separated GitHub usernames to assign to the PR. Defaults to none.

### Review requests

- `pr_reviewers` — Comma-separated GitHub usernames to request as reviewers. Only individual users can be specified.
- `pr_teams_reviewers` — Comma-separated team slugs (e.g., `backend`, `qa`) from the same org as the repo.  
  + Requires a token with `repo` and `read:org` scopes if the default `GITHUB_TOKEN` is restricted.

### Authentication

- `custom_github_token` — Optional token for creating/updating pull requests. Defaults to `GITHUB_TOKEN`. Use when elevated permissions are needed (assigning reviewers, interacting with protected branches, cross-repo changes). Keep secret.

### Platform support

- `os_platform` — Target platform for the precompiled binaries used by this action (`linux_amd64`, `linux_arm64`, `mac_amd64`, `mac_arm64`). These binaries handle tasks like downloading and processing translations. Typically, you don't need to change this, as the default (`linux_amd64`) works for most environments. Override if running on a macOS runner or a different architecture.

### Configuring GitHub permissions

1. Go to your repository's **Settings**.
2. Navigate to **Actions > General**.
3. Under **Workflow permissions**, set the permissions to **Read and write permissions**.
4. Enable **Allow GitHub Actions to create and approve pull requests** on the same page (under "Choose whether GitHub Actions can create pull requests or submit approving pull request reviews").

## Technical details

### Outputs

This action exposes the following outputs:

- **`created_branch`** — The branch used for the PR.  
  + On manual runs: a new temp branch is created.  
  + On PR runs: the PR head branch is reused.  
  + Empty if no changes were committed.
- **`pr_exists`** — `true` if a pull request exists after the run (either created or updated). Empty/false if no PR was touched.
- **`pr_created`** — `true` if a brand-new pull request was created by this run. Empty/false otherwise.
- **`pr_updated`** — `true` if an existing pull request was updated by this run. Empty/false otherwise.
- **`pr_action`** — String value: `"created"`, `"updated"`, or `"none"`. Convenience output to know what happened.
- **`pr_number`** — Number of the pull request (created or existing). Empty if no PR exists.
- **`pr_id`** — Node ID of the pull request (useful for GraphQL API calls).
- **`pr_url`** — URL of the pull request. Empty if no PR exists.

For example:

```yaml
- name: Debug outputs
  run: |
    echo "Branch used:   ${{ steps.lokalise-pull.outputs.created_branch }}"
    echo "PR exists:     ${{ steps.lokalise-pull.outputs.pr_exists }}"
    echo "PR created:    ${{ steps.lokalise-pull.outputs.pr_created }}"
    echo "PR updated:    ${{ steps.lokalise-pull.outputs.pr_updated }}"
    echo "PR action:     ${{ steps.lokalise-pull.outputs.pr_action }}"
    echo "PR number:     ${{ steps.lokalise-pull.outputs.pr_number }}"
    echo "PR id:         ${{ steps.lokalise-pull.outputs.pr_id }}"
    echo "PR url:        ${{ steps.lokalise-pull.outputs.pr_url }}"
```

### Required permissions

By default, this action requires the following permissions:

```yaml
permissions:
  contents: write
  pull-requests: write
```

Also, `issues: write` might be needed if you're providing the `pr_labels` parameter.

### How this action works

When triggered, this action follows these steps:

1. **Download translation files**:
   - Retrieves translation files for all languages from the specified Lokalise project.
   - The downloaded keys are filtered by the tag corresponding to the triggering branch. For example, if the branch is named `lokalise-hub`, only keys tagged with `lokalise-hub` in Lokalise will be included in the download bundle.

2. **Detect changes**:
   - Compares the downloaded translation files against the repository’s existing files to detect any updates or modifications.

3. **Create a pull request**:
   - If changes are detected, the action creates a pull request from a temporary branch to the triggering branch.
   - The temporary branch name is constructed using the prefix specified in the `temp_branch_prefix` parameter.

For more information on assumptions, refer to the [Assumptions and defaults](https://developers.lokalise.com/docs/github-actions#assumptions-and-defaults) section.

### Default parameters for the pull action

By default, the following headers and parameters are set when downloading files from Lokalise:

- `X-Api-Token` header — Derived from the `api_token` parameter.
- `project_id` GET param — Derived from the `project_id` parameter.
- `format` — Derived from the `file_format` parameter.
- `original_filenames` — Set to `true`.
- `directory_prefix` — Set to `/`.
- `include_tags` — Set to the branch name that triggered the workflow.

## Checksums and attestation

You'll find checksums for the compiled binaries in the `bin/` directory. The checksums are also signed and attested. To verify, install Cosign, clone the repo, and run the following commands in the project root:

```
cosign verify-blob-attestation --bundle bin/checksums.txt.attestation --certificate-identity "https://github.com/lokalise/lokalise-pull-action/.github/workflows/build-to-bin.yml@refs/tags/[INSERT_VERSION]" --certificate-oidc-issuer "https://token.actions.githubusercontent.com" --type custom bin/checksums.txt

cosign verify-blob --bundle bin/checksums.txt.sigstore --certificate-identity-regexp "^https://github.com/lokalise/lokalise-pull-action/\.github/workflows/build-to-bin\.yml@.*$" --certificate-oidc-issuer "https://token.actions.githubusercontent.com" bin/checksums.txt
```

## Special notes and known issues

* If you are using Gettext (PO files) and the action opens pull requests when no translations have been changed and the only difference is the "revision date", [refer to the following comment for clarifications](https://github.com/lokalise/lokalise-pull-action/issues/9#issuecomment-2578225342)
* If you are using iOS strings files, [please check the following document on our Developer Hub](https://developers.lokalise.com/docs/github-actions#support-for-ios-strings-files) containing setup recommendations

## License

Apache license version 2
