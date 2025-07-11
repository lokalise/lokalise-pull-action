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
        uses: lokalise/lokalise-pull-action@v3.10.0
        with:
          api_token: ${{ secrets.LOKALISE_API_TOKEN }}
          project_id: LOKALISE_PROJECT_ID
          base_lang: en
          translations_path: |
            TRANSLATIONS_PATH1
            TRANSLATIONS_PATH2
          file_format: json
          additional_params: |
            --indentation=2sp
            --export-empty-as=skip
            --export-sort=a_z
            --replace-breaks=false
            --language-mapping=[{"original_language_iso":"en_US","custom_language_iso":"en-US"}]
```

### Important note on Lokalise filenames and tags

Before running this action, ensure that your translation keys in Lokalise are properly assigned with relevant [filenames](https://docs.lokalise.com/en/articles/2281317-filenames) and [tags](https://docs.lokalise.com/en/articles/1475552-tags).

#### Tags and branch context

By default, the action filters downloaded keys based on a tag that matches the Git branch name used to trigger the workflow. This is done automatically via the `--include-tags=BRANCH_NAME` command-line argument.

For example, if the action is triggered from the `hub` branch on GitHub, it will download only the keys tagged with `hub`: `--include-tags=hub`. If no keys with the specified tag are found, the action will terminate.

To disable this automatic filtering or use custom tags, set the `skip_include_tags` parameter to `true`. You can also provide your own tags via the `additional_params` field:

```yaml
skip_include_tags: true
additional_params: |
  --include-tags=custom_tag
```

#### Filenames and directory structure

If you specify `locales` as the `translations_path`, your keys must include filenames that align with this structure, such as:

- `locales/%LANG_ISO%.json`
- `locales/%LANG_ISO%/main.xml`

Here:

- `%LANG_ISO%` will be replaced with the language code (e.g., `en`, `fr`, etc.).

If the filenames do not include the correct directory prefix (like `locales/`), the action will fail to compare the downloaded files with the existing files in your `translations_path`. In this case, the workflow logs will show the message: "No changes detected in translation files.".

To avoid this, double-check that your Lokalise filenames match the expected directory structure.

Note that by default the action adds the `--original-filenames=true` and `--directory-prefix=/` command line arguments. To disable this behavior, set the `skip_original_filenames` to `true`.

## Configuration

You'll need to provide some parameters for the action. These can be set as environment variables, secrets, or passed directly. Refer to the [General setup](https://developers.lokalise.com/docs/github-actions#general-setup-overview) section for detailed instructions.

### Mandatory parameters

- `api_token` — Lokalise API token with read/write permissions.
- `project_id` — Your Lokalise project ID.
- `translations_path` — One or more paths to your translation files. Do not provide the actual filenames here. For example, if your translations are stored in the `locales` folder at the project root, use `locales`. Defaults to `locales`.
- `file_format` — Defines the format of your translation files, such as `json` for JSON files. Defaults to `json`. This format determines how translation files are processed and also influences the file extension used when searching for them. However, some specific formats, such as `json_structured`, may still be downloaded with a generic `.json` extension. If you're using such a format, make sure to set the `file_ext` parameter explicitly to match the correct extension for your files.
- `base_lang` — Your project base language, such as `en` for English. Defaults to `en`.
- `file_ext` (*not strictly mandatory but still recommended*) — Custom file extension to use when searching for translation files (without leading dot, for example `json` or `yml`). By default, the extension is inferred from the `file_format` value. However, for certain formats (e.g., `json_structured`), the downloaded files may still have a generic extension. In such cases, this parameter allows specifying the correct extension manually to ensure proper file matching.

### File and CLI options

- `async_mode` — Download translations in asynchronous mode. Not recommended for small projects but required for larger ones (>= 10 000 key-language pairs). Defaults to `false`.
- `additional_params` — Extra parameters to pass to the [Lokalise CLI when pulling files](https://github.com/lokalise/lokalise-cli-2-go/blob/main/docs/lokalise2_file_download.md). For example, you can use `--indentation=2sp` to manage indentation. Defaults to an empty string. Multiple params can be specified:

```yaml
additional_params: |
  --indentation=2sp
  --export-empty-as=skip
  --export-sort=a_z
  --replace-breaks=false
  --language-mapping=[{"original_language_iso":"en_US","custom_language_iso":"en-US"}]
```

- `flat_naming` — Use flat naming convention. Set to `true` if your translation files follow a flat naming pattern like `locales/en.json` instead of `locales/en/file.json`. Defaults to `false`.
- `skip_include_tags` — Skip setting the `--include-tags` argument during download. This will download all translation keys for the specified format, regardless of tags.
- `skip_original_filenames` — Skips setting the `--original-filenames` and `--directory-prefix` arguments during download. You can disable original filenames by setting `--original-filenames=false` explicitly via `additional_params`.

### Git configuration

- `git_user_name` — Optional Git username to use when committing changes. If not provided, the action will default to the GitHub actor associated with the workflow run. Useful if you want to use a specific name (e.g. "Localization Bot") in your commit history.
- `git_user_email` — Optional Git email address to associate with commits. If not set, it defaults to a noreply address based on the Git username (e.g. `username@users.noreply.github.com`). This is helpful for cleaner commit metadata or if you want to associate commits with a bot/user email.

### Pull request and GitHub options

- `custom_github_token` — Optional GitHub token to use when creating or updating the pull request. If not provided, the default `GITHUB_TOKEN` is used. This can be helpful when your workflow requires elevated permissions (e.g., assigning reviewers, interacting with protected branches, or writing outside the current repo). Make sure to keep this token secret.
- `pr_labels` — Comma-separated list of labels to apply to the created pull request.
- `pr_title` — Title for the pull request. If not provided, defaults to "Translations update".
- `pr_body` — Body text for the pull request. If not provided, defaults to "This pull request updates translations from Lokalise".
- `override_branch_name` — Optional static branch name to use instead of auto-generating one. This is useful if you want the action to update the same pull request across multiple runs (e.g., always syncing to `lokalise-sync`). If the branch already exists, it will be checked out and updated instead of creating a new one.
- `force_push` — Whether to force push changes to the remote branch. Useful when using a static branch name and you want to overwrite any previous state (e.g., updating an existing PR). Set to `true` with caution, as this will overwrite history. Defaults to `false`.
- `git_commit_message` — Git commit message to use. If not provided, defaults to "Translations update".
- `pr_reviewers` — Optional comma-separated list of GitHub usernames to request as reviewers on the pull request. Only individual users can be specified here. Reviewers must have access to the repository.
- `pr_teams_reviewers` — Optional comma-separated list of team slugs (e.g., `backend`, `qa`) from the same GitHub organization that owns the repository. These teams will be requested as reviewers. Has no effect for repositories not under an organization, or if the teams are not part of the org.  
  + Requesting team reviewers requires a token with the `repo` and `read:org` scopes. If the default `GITHUB_TOKEN` is restricted by your organization, supply a custom Personal Access Token via `custom_github_token` that includes at least those scopes.

### Behavior tweaks and retries

- `temp_branch_prefix` — A prefix for the temporary branch used to create the pull request. For example, using `lok` will result in a branch name starting with `lok`. Defaults to `lok`.
- `always_pull_base` — By default, changes in the base language translation files (defined by the `base_lang` option) are ignored when checking for updates. Set this option to `true` to include changes in the base language translations in the pull request. Defaults to `false`.
- `max_retries` — Maximum number of retries on rate limit errors (HTTP 429). Defaults to `3`.
- `sleep_on_retry` — Number of seconds to sleep before retrying on rate limit errors. Defaults to `1`.
- `download_timeout` — Timeout for the download operation, in seconds. Defaults to `120`.

### Platform support

- `os_platform` — Target platform for the precompiled binaries used by this action (`linux_amd64`, `linux_arm64`, `mac_amd64`, `mac_arm64`). These binaries handle tasks like downloading and processing translations. Typically, you don't need to change this, as the default (`linux_amd64`) works for most environments. Override if running on a macOS runner or a different architecture.

### GitHub permissions

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

## Special notes and known issues

* If you are using Gettext (PO files) and the action opens pull requests when no translations have been changed and the only difference is the "revision date", [refer to the following comment for clarifications](https://github.com/lokalise/lokalise-pull-action/issues/9#issuecomment-2578225342)

## License

Apache license version 2
