module.exports = async ({ github, context }) => {
  const { repo } = context;

  try {
    const branchName = process.env.BRANCH_NAME;
    const baseRef = process.env.BASE_REF;
    const prLabels = (process.env.PR_LABELS || "")
      .split(',')
      .map(label => label.trim())
      .filter(label => label.length > 0);

    if (!branchName || !baseRef) {
      throw new Error("Required environment variables are missing");
    }

    // List existing PRs
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
        },
      };
    }

    const { data: newPr } = await github.rest.pulls.create({
      owner: repo.owner,
      repo: repo.repo,
      title: "Lokalise translations update",
      head: branchName,
      base: baseRef,
      body: "This PR updates translations from Lokalise.",
    });

    if (prLabels.length > 0) {
      await github.rest.issues.addLabels({
        owner: repo.owner,
        repo: repo.repo,
        issue_number: newPr.number,
        labels: prLabels,
      });
    }

    console.log(`Created new PR: ${newPr.html_url}`);
    console.log(newPr);
    return {
      created: true,
      pr: {
        number: newPr.number,
      },
    };
  } catch (error) {
    console.error(`Failed to create or update pull request: ${error.message}`);
    return {
      created: false,
    };
  }
};
