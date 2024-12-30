# GitHub action to pull translation files from Lokalise

GitHub action to download translation files from [Lokalise TMS](https://lokalise.com/) to your GitHub repository in the form of a pull request.

**Step-by-step tutorial covering the usage of this action is available on [Lokalise Developer Hub](https://developers.lokalise.com/docs/github-actions).** To upload translation files from GitHub to Lokalise, use the [lokalise-push-action](https://github.com/lokalise/lokalise-push-action).

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
        uses: actions/checkout@v4
        with:
          fetch-depth: 0

      - name: Pull from Lokalise
        uses: lokalise/lokalise-pull-action@v3.1.0
        with:
          api_token: ${{ secrets.LOKALISE_API_TOKEN }}
          project_id: LOKALISE_PROJECT_ID
          translations_path: |
            TRANSLATIONS_PATH1
            TRANSLATIONS_PATH2
          file_format: FILE_FORMAT
          additional_params: ADDITIONAL_CLI_PARAMS
```

### Important note on Lokalise filenames and tags

Before running this action, ensure that your translation keys on Lokalise are correctly assigned with appropriate [filenames](https://docs.lokalise.com/en/articles/2281317-filenames) and [tags](https://docs.lokalise.com/en/articles/1475552-tags).

#### Tags and branch context

If you are running this action from the `hub` branch on GitHub, the action will download only the keys that have the `hub` tag assigned. If no such keys are found, the action will halt execution.

#### Filenames and directory structure

If you specify `locales` as the `translations_path`, your keys must include filenames that align with this structure, such as:

- `locales/%LANG_ISO%.json`
- `locales/%LANG_ISO%/main.xml`

Here:

- `%LANG_ISO%` will be replaced with the language code (e.g., `en`, `fr`, etc.).

If the filenames do not include the correct directory prefix (like `locales/`), the action will fail to compare the downloaded files with the existing files in your `translations_path`. In this case, the workflow logs will show the message: "No changes detected in translation files.".

To avoid this, double-check that your Lokalise filenames match the expected directory structure.

## Configuration

### Parameters

You'll need to provide some parameters for the action. These can be set as environment variables, secrets, or passed directly. Refer to the [General setup](https://developers.lokalise.com/docs/github-actions#general-setup-overview) section for detailed instructions.

#### Mandatory parameters

- `api_token` — Lokalise API token with read/write permissions.
- `project_id` — Your Lokalise project ID.
- `translations_path` — One or more paths to your translation files. For example, if your translations are stored in the `locales` folder at the project root, use `locales`. Defaults to `locales`.
- `file_format` — The format of your translation files, such as `json` for JSON files. Defaults to `json`.
- `base_lang` — Your project base language, such as `en` for English. Defaults to `en`.

#### Optional parameters

- `additional_params` — Extra parameters to pass to the [Lokalise CLI when pulling files](https://github.com/lokalise/lokalise-cli-2-go/blob/main/docs/lokalise2_file_download.md). For example, you can use `--indentation 2sp` to manage indentation. Multiple CLI arguments can be added, such as `--indentation 2sp --placeholder-format icu`. Defaults to an empty string.
- `temp_branch_prefix` — A prefix for the temporary branch used to create the pull request. For example, using `lok` will result in a branch name starting with `lok`. Defaults to `lok`.
- `always_pull_base` — By default, changes in the base language translation files (defined by the `base_lang` option) are ignored when checking for updates. Set this option to `true` to include changes in the base language translations in the pull request. Defaults to `false`.
- `flat_naming` — Use flat naming convention. Set to `true` if your translation files follow a flat naming pattern like `locales/en.json` instead of `locales/en/file.json`. Defaults to `false`.
- `skip_include_tags` — Skip setting the `--include-tags` argument during download. This will download all translation keys for the specified format, regardless of tags. You can also provide custom filtering options via `additional_params`, for example `--include-tags staging,dev`.
- `max_retries` — Maximum number of retries on rate limit errors (HTTP 429). Defaults to `3`.
- `sleep_on_retry` — Number of seconds to sleep before retrying on rate limit errors. Defaults to `1`.
- `download_timeout` — Timeout for the download operation, in seconds. Defaults to `120`.

### Permissions

1. Go to your repository's **Settings**.
2. Navigate to **Actions > General**.
3. Under **Workflow permissions**, set the permissions to **Read and write permissions**.
4. Enable **Allow GitHub Actions to create and approve pull requests** on the same page (under "Choose whether GitHub Actions can create pull requests or submit approving pull request reviews").

## Technical details

### Outputs

This action has the following outputs:

- `created_branch` — The name of the branch that was created and used for the pull request. Empty if no branch has been created (for example, if no changes have been detected).
- `pr_created` — A boolean value specifying whether a pull request with translation updates was created. False when there are no changes or something went wrong.
- `pr_number` —  Number of the created pull request.
- `pr_id` — ID of the created pull request.

For example:

```yaml
- name: Debug
  run: |
    echo "Branch created: ${{ steps.lokalise-pull.outputs.created_branch }}"
    echo "PR created: ${{ steps.lokalise-pull.outputs.pr_created }}"
    echo "PR id: ${{ steps.lokalise-pull.outputs.pr_id }}"
    echo "PR number: ${{ steps.lokalise-pull.outputs.pr_number }}"
```

### How this action works

When triggered, this action follows these steps:

1. **Install Lokalise CLIv2**:
   - Ensures that the required Lokalise CLI is available for subsequent operations.

2. **Download translation files**:
   - Retrieves translation files for all languages from the specified Lokalise project.
   - The downloaded keys are filtered by the tag corresponding to the triggering branch. For example, if the branch is named `lokalise-hub`, only keys tagged with `lokalise-hub` in Lokalise will be included in the download bundle.

3. **Detect changes**:
   - Compares the downloaded translation files against the repository’s existing files to detect any updates or modifications.

4. **Create a pull request**:
   - If changes are detected, the action creates a pull request from a temporary branch to the triggering branch.
   - The temporary branch name is constructed using the prefix specified in the `temp_branch_prefix` parameter.

For more information on assumptions, refer to the [Assumptions and defaults](https://developers.lokalise.com/docs/github-actions#assumptions-and-defaults) section.

### Default parameters for the pull action

By default, the following command-line parameters are set when downloading files from Lokalise:

- `--token` — Derived from the `api_token` parameter.
- `--project-id` — Derived from the `project_id` parameter.
- `--format` — Derived from the `file_format` parameter.
- `--original-filenames` — Set to `true`.
- `--directory-prefix` — Set to `/`.
- `--include-tags` — Set to the branch name that triggered the workflow.

## License

Apache license version 2