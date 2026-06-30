// Purpose:
//   Create a PR if it doesn't exist yet for the given head branch,
//   or update metadata (title / body / draft / labels / reviewers / assignees) if it does.
//
// Design notes:
//   - Fork-aware when listing/creating PRs, assuming the commit step has already pushed the head branch.
//   - Synthetic BASE_REF (PR merge/head refs) are replaced with repo default branch.
//   - Gracefully tolerates missing permissions for reviewers/assignees/labels (warns, doesn't fail).
//   - Hard failures in PR lookup/creation are re-thrown so the GitHub Actions step fails clearly.

function parseBool(value) {
  return String(value || "false").trim().toLowerCase() === "true";
}

function parseCommaList(value) {
  return String(value || "")
    .split(",")
    .map((s) => s.trim())
    .filter(Boolean);
}

function isSyntheticRef(ref) {
  const value = String(ref || "").trim().toLowerCase();

  return (
    !value ||
    value === "merge" ||
    value === "head" ||
    /^(\d+)\/(merge|head)$/.test(value) ||
    value.startsWith("refs/pull/") ||
    value.startsWith("pull/") ||
    value.endsWith("/merge") ||
    value.endsWith("/head")
  );
}

async function resolveBaseRef({ github, repo, rawBaseRef }) {
  let baseRef = String(rawBaseRef || "")
    .trim()
    .replace(/^refs\/heads\//, "");

  if (!isSyntheticRef(baseRef)) {
    return baseRef;
  }

  const { data: repoInfo } = await github.rest.repos.get({
    owner: repo.owner,
    repo: repo.repo,
  });

  baseRef = repoInfo.default_branch;
  console.log(`BASE_REF was invalid/synthetic, using default branch: ${baseRef}`);

  return baseRef;
}

function resolveHeadRefs({ context, repo, branchName }) {
  const payload = context.payload || {};

  const headRepoFullName =
    payload.pull_request?.head?.repo?.full_name || `${repo.owner}/${repo.repo}`;

  const headOwner = headRepoFullName.split("/")[0];
  const sameRepo = headRepoFullName === `${repo.owner}/${repo.repo}`;

  return {
    headForList: `${headOwner}:${branchName}`,
    headForCreate: sameRepo ? branchName : `${headOwner}:${branchName}`,
    sameRepo,
  };
}

async function convertPullRequestToDraft(github, prNodeId) {
  await github.graphql(
    `
      mutation($pullRequestId: ID!) {
        convertPullRequestToDraft(input: { pullRequestId: $pullRequestId }) {
          pullRequest {
            id
            isDraft
          }
        }
      }
    `,
    {
      pullRequestId: prNodeId,
    },
  );
}

async function updateExistingPullRequest({
  github,
  repo,
  prNumber,
  prTitle,
  prBody,
}) {
  try {
    await github.rest.pulls.update({
      owner: repo.owner,
      repo: repo.repo,
      pull_number: prNumber,
      title: prTitle,
      body: prBody,
    });

    console.log("Updated existing PR title/body.");
  } catch (err) {
    console.warn(`Cannot update PR title/body: ${err.message}`);
  }
}

async function applyLabels({ github, repo, prNumber, labels }) {
  if (!labels.length) {
    return;
  }

  try {
    await github.rest.issues.addLabels({
      owner: repo.owner,
      repo: repo.repo,
      issue_number: prNumber,
      labels,
    });

    console.log("Labels added.");
  } catch (err) {
    console.warn(`Cannot add labels: ${err.message}`);
  }
}

async function requestReviewers({ github, repo, prNumber, reviewers, teams }) {
  if (!reviewers.length && !teams.length) {
    return;
  }

  try {
    await github.rest.pulls.requestReviewers({
      owner: repo.owner,
      repo: repo.repo,
      pull_number: prNumber,
      reviewers,
      team_reviewers: teams,
    });

    console.log("Reviewers requested.");
  } catch (err) {
    console.warn(`Cannot add reviewers: ${err.message}`);
  }
}

async function addAssignees({ github, repo, prNumber, assignees }) {
  if (!assignees.length) {
    return;
  }

  try {
    await github.rest.issues.addAssignees({
      owner: repo.owner,
      repo: repo.repo,
      issue_number: prNumber,
      assignees,
    });

    console.log("Assignees added.");
  } catch (err) {
    console.warn(`Cannot add assignees: ${err.message}`);
  }
}

async function applyPullRequestMetadata({
  github,
  repo,
  prNumber,
  labels,
  reviewers,
  teams,
  assignees,
}) {
  await applyLabels({ github, repo, prNumber, labels });
  await requestReviewers({ github, repo, prNumber, reviewers, teams });
  await addAssignees({ github, repo, prNumber, assignees });
}

module.exports = async ({ github, context }) => {
  console.log("Creating or updating PR...");

  const { repo } = context;

  try {
    const rawBaseRef = process.env.BASE_REF || "";
    const branchName = String(process.env.BRANCH_NAME || "").trim();

    const prTitle = process.env.PR_TITLE || "Lokalise: sync translations";
    const prBody = process.env.PR_BODY || "";
    const prDraft = parseBool(process.env.PR_DRAFT);

    const prLabels = parseCommaList(process.env.PR_LABELS);
    const prReviewers = parseCommaList(process.env.PR_REVIEWERS);
    const prTeams = parseCommaList(process.env.PR_TEAMS_REVIEWERS);
    const prAssignees = parseCommaList(process.env.PR_ASSIGNEES);

    if (!branchName) {
      throw new Error("BRANCH_NAME is missing");
    }

    const baseRef = await resolveBaseRef({
      github,
      repo,
      rawBaseRef,
    });

    const { headForList, headForCreate } = resolveHeadRefs({
      context,
      repo,
      branchName,
    });

    console.log(`Resolved base: ${baseRef}`);
    console.log(`Resolved head (list): ${headForList}`);
    console.log(`Resolved head (create): ${headForCreate}`);

    const { data: pullRequests } = await github.rest.pulls.list({
      owner: repo.owner,
      repo: repo.repo,
      state: "open",
      head: headForList,
      base: baseRef,
      per_page: 1,
    });

    if (pullRequests.length > 0) {
      const existing = pullRequests[0];
      const prNumber = existing.number;

      console.log(`PR already exists: ${existing.html_url}`);

      await updateExistingPullRequest({
        github,
        repo,
        prNumber,
        prTitle,
        prBody,
      });

      if (prDraft && !existing.draft) {
        try {
          await convertPullRequestToDraft(github, existing.node_id);
          console.log("Converted existing PR to draft.");
        } catch (err) {
          console.warn(`Cannot convert to draft: ${err.message}`);
        }
      }

      await applyPullRequestMetadata({
        github,
        repo,
        prNumber,
        labels: prLabels,
        reviewers: prReviewers,
        teams: prTeams,
        assignees: prAssignees,
      });

      return {
        created: false,
        updated: true,
        pr: {
          number: prNumber,
          id: existing.id,
          html_url: existing.html_url,
        },
      };
    }

    const { data: newPr } = await github.rest.pulls.create({
      owner: repo.owner,
      repo: repo.repo,
      title: prTitle,
      head: headForCreate,
      base: baseRef,
      body: prBody,
      draft: prDraft,
      maintainer_can_modify: true,
    });

    await applyPullRequestMetadata({
      github,
      repo,
      prNumber: newPr.number,
      labels: prLabels,
      reviewers: prReviewers,
      teams: prTeams,
      assignees: prAssignees,
    });

    console.log(`Created new PR: ${newPr.html_url}`);

    return {
      created: true,
      updated: false,
      pr: {
        number: newPr.number,
        id: newPr.id,
        html_url: newPr.html_url,
      },
    };
  } catch (error) {
    console.error(`Failed to create or update pull request: ${error.message}`);
    throw error;
  }
};