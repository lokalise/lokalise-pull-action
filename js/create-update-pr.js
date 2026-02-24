// Purpose:
//   Create a PR if it doesn't exist yet for the given head branch,
//   or update metadata (draft / labels / reviewers / assignees) if it does.
// Design notes:
//   - Fork-aware: uses "owner:branch" head filter when listing PRs.
//   - Synthetic BASE_REF (PR merge/head refs) are replaced with repo default branch.
//   - Gracefully tolerates missing permissions for reviewers/assignees (warns, doesn't fail).
//   - Returns a compact result usable by the composite action outputs.

module.exports = async ({ github, context }) => {
  console.log("Creating or updating PR...");

  const { repo } = context; // { owner, repo }

  try {
    // ─────────── Read and normalize inputs ───────────
    let baseRef = (process.env.BASE_REF || "").trim();
    const branchName = (process.env.BRANCH_NAME || "").trim();
    const prTitle = process.env.PR_TITLE || "Lokalise: sync translations";
    const prBody = process.env.PR_BODY || "";
    const prDraft =
      String(process.env.PR_DRAFT || "false").toLowerCase() === "true";

    // Comma-separated env vars → arrays with trimming; empty pieces dropped.
    const prLabels = (process.env.PR_LABELS || "")
      .split(",")
      .map((s) => s.trim())
      .filter(Boolean);
    const prReviewers = (process.env.PR_REVIEWERS || "")
      .split(",")
      .map((s) => s.trim())
      .filter(Boolean);
    const prTeams = (process.env.PR_TEAMS_REVIEWERS || "")
      .split(",")
      .map((s) => s.trim())
      .filter(Boolean);
    const prAssignees = (process.env.PR_ASSIGNEES || "")
      .split(",")
      .map((s) => s.trim())
      .filter(Boolean);

    if (!branchName) {
      // Hard requirement: the commit step must pass branch_name as BRANCH_NAME.
      throw new Error("BRANCH_NAME is missing");
    }

    // Normalize refs: strip "refs/heads/" in case the caller passed full ref.
    baseRef = baseRef.replace(/^refs\/heads\//, "");

    // CI often feeds synthetic refs for PR events (e.g. "123/merge").
    const looksLikeSynthetic =
      /^(\d+)\/(merge|head)$/.test(baseRef) ||
      baseRef === "merge" ||
      baseRef === "head";

    if (!baseRef || looksLikeSynthetic) {
      // Fall back to repository default branch (e.g., "main"). Robust for non-standard setups.
      const { data: repoInfo } = await github.rest.repos.get({
        owner: repo.owner,
        repo: repo.repo,
      });
      baseRef = repoInfo.default_branch;
      console.log(
        `BASE_REF was invalid/synthetic, using default branch: ${baseRef}`,
      );
    }

    // ─────────── Detect head owner (fork-safe) ───────────
    // If the workflow runs on a PR from a fork, the PR head repo can be different from the base repo.
    // For listing PRs we MUST use "owner:branch" in the 'head' filter (GitHub API constraint).
    const payload = context.payload || {};
    const headRepoFullName =
      payload.pull_request?.head?.repo?.full_name ||
      `${repo.owner}/${repo.repo}`; // same-repo default

    const headOwner = headRepoFullName.split("/")[0];

    // For listing PRs, always "owner:branch".
    const headForList = `${headOwner}:${branchName}`;
    // For creating PRs, same-repo can pass just "branch", fork requires "owner:branch".
    const sameRepo = headRepoFullName === `${repo.owner}/${repo.repo}`;
    const headForCreate = sameRepo ? branchName : headForList;

    console.log(`Resolved base: ${baseRef}`);
    console.log(`Resolved head (list): ${headForList}`);
    console.log(`Resolved head (create): ${headForCreate}`);

    /* ─────────── CHECK FOR EXISTING PR ───────────
       We look for an open PR with the exact head (owner:branch) into the base.
       per_page=1 is enough; there should be at most one such PR by convention. */
    const { data: pullRequests } = await github.rest.pulls.list({
      owner: repo.owner,
      repo: repo.repo,
      state: "open",
      head: headForList,
      base: baseRef,
      per_page: 1,
    });

    if (pullRequests.length > 0) {
      // ─────────── UPDATE EXISTING PR ───────────
      const existing = pullRequests[0];
      const prNumber = existing.number;

      console.log(`PR already exists: ${existing.html_url}`);

      // Convert to draft (no-op if already draft). Failure tolerated (permissions / repo settings).
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

      // Apply labels if provided (idempotent; GH dedupes).
      if (prLabels.length) {
        try {
          await github.rest.issues.addLabels({
            owner: repo.owner,
            repo: repo.repo,
            issue_number: prNumber,
            labels: prLabels,
          });
        } catch (err) {
          console.warn(`Cannot add labels: ${err.message}`);
        }
      }

      // Request reviewers (users/teams). Some combos can fail (e.g., missing org perms).
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

      // Assignees (fails harmlessly if users lack access).
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

      return {
        created: false,
        pr: { number: prNumber, id: existing.id, html_url: existing.html_url },
      };
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
      maintainer_can_modify: true, // helpful on forks so maintainers can adjust branch
    });

    // Labels after creation (API limitation: labels on issues endpoint).
    if (prLabels.length) {
      try {
        await github.rest.issues.addLabels({
          owner: repo.owner,
          repo: repo.repo,
          issue_number: newPr.number,
          labels: prLabels,
        });
      } catch (err) {
        console.warn(`Cannot add labels: ${err.message}`);
      }
    }

    // Request reviewers (tolerate failures).
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

    // Assignees (tolerate failures).
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
    return {
      created: true,
      pr: { number: newPr.number, id: newPr.id, html_url: newPr.html_url },
    };
  } catch (error) {
    // We deliberately do not throw here as the composite action expects a structured return.
    console.error(`Failed to create or update pull request: ${error.message}`);
    return { created: false, pr: null };
  }
};
