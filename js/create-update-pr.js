// create-update-pr.js
module.exports = async ({ github, context }) => {
  const { repo } = context;
  console.log("Creating or updating PR...");

  try {
    const branchName = process.env.BRANCH_NAME;
    const baseRef = process.env.BASE_REF;
    const prTitle = process.env.PR_TITLE;
    const prBody = process.env.PR_BODY;

    if (!branchName || !baseRef) {
      throw new Error("Required environment variables are missing");
    }

    const prLabels = (process.env.PR_LABELS || "")
      .split(",").map(s => s.trim()).filter(Boolean);

    const prReviewers = (process.env.PR_REVIEWERS || "")
      .split(",").map(s => s.trim()).filter(Boolean);

    const prTeams = (process.env.PR_TEAMS_REVIEWERS || "")
      .split(",").map(s => s.trim()).filter(Boolean);

    const prAssignees = (process.env.PR_ASSIGNEES || "")
      .split(",").map(s => s.trim()).filter(Boolean);

    const prDraft = String(process.env.PR_DRAFT || "false").toLowerCase() === "true";

    /* ─────────── CHECK FOR EXISTING PR ─────────── */
    const { data: pullRequests } = await github.rest.pulls.list({
      owner: repo.owner,
      repo: repo.repo,
      head: `${repo.owner}:${branchName}`,
      base: baseRef,
      state: "open",
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

      return { created: false, pr: { number: prNumber, id: existing.id } };
    }

    /* ─────────── CREATE PR ─────────── */
    const { data: newPr } = await github.rest.pulls.create({
      owner: repo.owner,
      repo: repo.repo,
      title: prTitle,
      head: branchName,
      base: baseRef,
      body: prBody,
      draft: prDraft,
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
    return { created: true, pr: { number: newPr.number, id: newPr.id } };
  } catch (error) {
    console.error(`Failed to create or update pull request: ${error.message}`);
    return { created: false };
  }
};
