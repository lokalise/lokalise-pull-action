module.exports = async ({ github, context }) => {
  const { repo, payload } = context;

  try {
    const branchName = process.env.BRANCH_NAME;
    const baseRef = process.env.BASE_REF;
    const token = process.env.GITHUB_TOKEN;

    if (!branchName || !baseRef || !token) {
      throw new Error("Required environment variables are missing");
    }

    const octokit = github.getOctokit(token);

    // List existing PRs
    const { data: pullRequests } = await octokit.rest.pulls.list({
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
    const { data: newPr } = await octokit.rest.pulls.create({
      owner: repo.owner,
      repo: repo.repo,
      title: "Lokalise translations update",
      head: branchName,
      base: baseRef,
      body: "This PR updates translations from Lokalise.",
    });

    console.log(`Created new PR: ${newPr.html_url}`);
    return { prUrl: newPr.html_url };
  } catch (error) {
    throw new Error(`Failed to create or update pull request: ${error.message}`);
  }
};
