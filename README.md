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
        uses: lokalise/lokalise-pull-action@v2.0.0
        with:
          api_token: ${{ secrets.LOKALISE_API_TOKEN }}
          project_id: LOKALISE_PROJECT_ID
          translations_path: |
            TRANSLATIONS_PATH1
          file_format: FILE_FORMAT
          additional_params: ADDITIONAL_CLI_PARAMS
```

## Configuration

### Parameters

You'll need to provide some parameters for the action. These can be set as environment variables, secrets, or passed directly. Refer to the [General setup](https://developers.lokalise.com/docs/github-actions#general-setup-overview) section for detailed instructions.

The following parameters are **mandatory**:

- `api_token` — Lokalise API token.
- `project_id` — Your Lokalise project ID.
- `translations_path` — One or more paths to your translation files.
- `file_format` — The format of your translation files.
- `base_lang` — Your project base language.

**Optional** parameters include:

- `additional_params` — Extra parameters to pass to the [Lokalise CLI when pulling files](https://github.com/lokalise/lokalise-cli-2-go/blob/main/docs/lokalise2_file_download.md). For example, you can use `--indentation 2sp` to manage indentation. Multiple CLI arguments can be added, like: `--indentation 2sp --placeholder-format icu`.
- `temp_branch_prefix` — A prefix for the temporary branch used to create the pull request. This value will be part of the branch name. For example, using `lok` will result in a branch name starting with `lok`. The default value is `lok`.
- `always_pull_base` — By default, changes in the base language translation files (defined by the `base_lang` option) are ignored when checking for updates. Set this option to `true` to include changes in the base language translations in the pull request. The default value is `false`.
* `max_retries` — Maximum number of retries on rate limit errors (HTTP 429). The default value is `3`.
* `sleep_on_retry` — Number of seconds to sleep before retrying on rate limit errors. The default value is `1`.

### Permissions

1. Go to your repository's **Settings**.
2. Navigate to **Actions > General**.
3. Under **Workflow permissions**, set the permissions to **Read and write permissions**.
4. Enable **Allow GitHub Actions to create and approve pull requests** on the same page (under "Choose whether GitHub Actions can create pull requests or submit approving pull request reviews").

## Technical details

### How this action works

When triggered, this action performs the following steps:

1. Installs Lokalise CLIv2.
2. Downloads translation files for all languages from the specified Lokalise project. The keys included in the download bundle are filtered by the tag named after the triggering branch. For example, if the branch is called `lokalise-hub`, only the keys with this tag will be downloaded.
3. If any changes in the translation files are detected, a pull request will be created for the triggering branch. This pull request will be sent from a temporary branch.

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