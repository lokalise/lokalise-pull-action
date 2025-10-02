// create-update-pr.js
module.exports = async ({ github, context }) => {
  console.log("Creating or updating PR...");

  const { repo } = context;

  try {
    let baseRef = (process.env.BASE_REF || "").trim();
    const branchName = (process.env.BRANCH_NAME || "").trim();
    const prTitle = process.env.PR_TITLE || "Lokalise: sync translations";
    const prBody = process.env.PR_BODY || "";
    const prDraft = String(process.env.PR_DRAFT || "false").toLowerCase() === "true";

    const prLabels = (process.env.PR_LABELS || "").split(",").map(s => s.trim()).filter(Boolean);
    const prReviewers = (process.env.PR_REVIEWERS || "").split(",").map(s => s.trim()).filter(Boolean);
    const prTeams = (process.env.PR_TEAMS_REVIEWERS || "").split(",").map(s => s.trim()).filter(Boolean);
    const prAssignees = (process.env.PR_ASSIGNEES || "").split(",").map(s => s.trim()).filter(Boolean);

    if (!branchName) {
      throw new Error("BRANCH_NAME is missing");
    }

    baseRef = baseRef.replace(/^refs\/heads\//, "");

    // synthetic PR refs? bail to default
    const looksLikeSynthetic = /^(\d+)\/(merge|head)$/.test(baseRef) || baseRef === "merge" || baseRef === "head";

    if (!baseRef || looksLikeSynthetic) {
      const { data: repoInfo } = await github.rest.repos.get({ owner: repo.owner, repo: repo.repo });
      baseRef = repoInfo.default_branch;
      console.log(`BASE_REF was invalid/synthetic, using default branch: ${baseRef}`);
    }

    // Head owner/repo detection (fork-aware, but harmless for same-repo)
    const payload = context.payload || {};
    const headRepoFullName =
      payload.pull_request?.head?.repo?.full_name ||
      `${repo.owner}/${repo.repo}`;
    const headOwner = headRepoFullName.split("/")[0];

    // For listing PRs, GitHub requires "owner:branch" in head filter
    const headForList = `${headOwner}:${branchName}`;
    // For creating PR, same-repo can be just "branch", fork must be "owner:branch"
    const sameRepo = headRepoFullName === `${repo.owner}/${repo.repo}`;
    const headForCreate = sameRepo ? branchName : headForList;

    console.log(`Resolved base: ${baseRef}`);
    console.log(`Resolved head (list): ${headForList}`);
    console.log(`Resolved head (create): ${headForCreate}`);

    /* ─────────── CHECK FOR EXISTING PR ─────────── */
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

      // convert to draft if requested
      if (prDraft && !existing.draft) {
        try {
          await github.rest.pulls.update({
            owner: repo.owner,
            repo: repo.repo,
            pull_number: prNumber,
            draft: true,
          });
          console.log("Converted existing PR to draft.");
        } catch (err) {
          console.warn(`Cannot convert to draft: ${err.message}`);
        }
      }

      // labels
      if (prLabels.length) {
        await github.rest.issues.addLabels({
          owner: repo.owner,
          repo: repo.repo,
          issue_number: prNumber,
          labels: prLabels,
        });
      }

      // reviewers (users/teams)
      if (prReviewers.length || prTeams.length) {
        try {
          await github.rest.pulls.requestReviewers({
            owner: repo.owner,
            repo: repo.repo,
            pull_number: prNumber,
            reviewers: prReviewers,
            team_reviewers: prTeams,
          });
        } catch (err) {
          console.warn(`Cannot add reviewers: ${err.message}`);
        }
      }

      // assignees
      if (prAssignees.length) {
        try {
          await github.rest.issues.addAssignees({
            owner: repo.owner,
            repo: repo.repo,
            issue_number: prNumber,
            assignees: prAssignees,
          });
          console.log("Assignees added.");
        } catch (err) {
          console.warn(`Cannot add assignees: ${err.message}`);
        }
      }

      return { created: false, pr: { number: prNumber, id: existing.id, html_url: existing.html_url } };
    }

    /* ─────────── CREATE PR ─────────── */
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

    // labels
    if (prLabels.length) {
      await github.rest.issues.addLabels({
        owner: repo.owner,
        repo: repo.repo,
        issue_number: newPr.number,
        labels: prLabels,
      });
    }

    // reviewers
    if (prReviewers.length || prTeams.length) {
      try {
        await github.rest.pulls.requestReviewers({
          owner: repo.owner,
          repo: repo.repo,
          pull_number: newPr.number,
          reviewers: prReviewers,
          team_reviewers: prTeams,
        });
      } catch (err) {
        console.warn(`Cannot add reviewers: ${err.message}`);
      }
    }

    // assignees
    if (prAssignees.length) {
      try {
        await github.rest.issues.addAssignees({
          owner: repo.owner,
          repo: repo.repo,
          issue_number: newPr.number,
          assignees: prAssignees,
        });
        console.log("Assignees added.");
      } catch (err) {
        console.warn(`Cannot add assignees: ${err.message}`);
      }
    }

    console.log(`Created new PR: ${newPr.html_url}`);
    return { created: true, pr: { number: newPr.number, id: newPr.id, html_url: newPr.html_url } };
  } catch (error) {
    console.error(`Failed to create or update pull request: ${error.message}`);
    return { created: false, pr: null };
  }
};
