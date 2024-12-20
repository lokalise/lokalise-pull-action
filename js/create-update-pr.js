module.exports = async ({ github, context }) => {
  const { repo, _payload } = context;

  try {
    const branchName = process.env.BRANCH_NAME;
    const baseRef = process.env.BASE_REF;

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
      return { prUrl: pullRequests[0].html_url };
    }

    // Create a new PR
    const { data: newPr } = await github.rest.pulls.create({
      owner: repo.owner,
      repo: repo.repo,
      title: "Lokalise translations update",
      head: branchName,
      base: baseRef,
      body: "This PR updates translations from Lokalise.",
      labels: ['automerge'],
    });

    console.log(`Created new PR: ${newPr.html_url}`);
  } catch (error) {
    throw new Error(`Failed to create or update pull request: ${error.message}`);
  }
};
