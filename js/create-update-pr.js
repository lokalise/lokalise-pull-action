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
      .split(",")
      .map(l => l.trim())
      .filter(Boolean);

    const prReviewers = (process.env.PR_REVIEWERS || "")
      .split(",")
      .map(r => r.trim())
      .filter(Boolean);

    const prTeams = (process.env.PR_TEAMS_REVIEWERS || "")
      .split(",")
      .map(t => t.trim())
      .filter(Boolean);

    /* ─────────── CHECK FOR EXISTING PR ─────────── */
    const { data: pullRequests } = await github.rest.pulls.list({
      owner: repo.owner,
      repo: repo.repo,
      head: `${repo.owner}:${branchName}`,
      base: baseRef,
      state: "open",
    });

    if (pullRequests.length > 0) {
      console.log(`PR already exists: ${pullRequests[0].html_url}`);
      return {
        created: false,
        pr: {
          number: pullRequests[0].number,
          id: pullRequests[0].id,
        },
      };
    }

    /* ─────────── CREATE PR ─────────── */
    const { data: newPr } = await github.rest.pulls.create({
      owner: repo.owner,
      repo: repo.repo,
      title: prTitle,
      head: branchName,
      base: baseRef,
      body: prBody,
    });

    /* ─────────── ADD LABELS ─────────── */
    if (prLabels.length) {
      await github.rest.issues.addLabels({
        owner: repo.owner,
        repo: repo.repo,
        issue_number: newPr.number,
        labels: prLabels,
      });
    }

    /* ─────────── REQUEST REVIEWERS ─────────── */
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
        console.warn(`Cannot add team reviewers: ${err.message}`);
      }
    }

    console.log(`Created new PR: ${newPr.html_url}`);

    return {
      created: true,
      pr: {
        number: newPr.number,
        id: newPr.id,
      },
    };
  } catch (error) {
    console.error(`Failed to create or update pull request: ${error.message}`);
    return { created: false };
  }
};
