package github

import (
	"context"
	"net/url"
	"strings"

	"github.com/google/go-github/v62/github"
	"golang.org/x/oauth2"
)

type Client struct {
	client *github.Client
	ctx    context.Context
}

type Repository struct {
	Owner         string
	Name          string
	CloneURL      string
	SSHUrl        string
	DefaultBranch string
	Archived      bool
}

type Branch struct {
	Name string
}

func NewClient(ctx context.Context, baseURL, token string) (*Client, error) {
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	tc := oauth2.NewClient(ctx, ts)

	var client *github.Client
	var err error

	if baseURL != "" && baseURL != "https://api.github.com" {
		// GitHub Enterprise
		if !strings.HasSuffix(baseURL, "/") {
			baseURL += "/"
		}
		client, err = github.NewClient(tc).WithEnterpriseURLs(baseURL, baseURL)
		if err != nil {
			return nil, err
		}
	} else {
		client = github.NewClient(tc)
	}

	return &Client{client: client, ctx: ctx}, nil
}

func (c *Client) ListOrganizations() ([]string, error) {
	var allOrgs []string
	opts := &github.ListOptions{PerPage: 100}

	for {
		orgs, resp, err := c.client.Organizations.List(c.ctx, "", opts)
		if err != nil {
			return nil, err
		}

		for _, org := range orgs {
			allOrgs = append(allOrgs, org.GetLogin())
		}

		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	return allOrgs, nil
}

func (c *Client) ListUserRepos() ([]Repository, error) {
	var allRepos []Repository
	opts := &github.RepositoryListByAuthenticatedUserOptions{
		ListOptions: github.ListOptions{PerPage: 100},
	}

	for {
		repos, resp, err := c.client.Repositories.ListByAuthenticatedUser(c.ctx, opts)
		if err != nil {
			return nil, err
		}

		for _, repo := range repos {
			allRepos = append(allRepos, Repository{
				Owner:         repo.GetOwner().GetLogin(),
				Name:          repo.GetName(),
				CloneURL:      repo.GetCloneURL(),
				SSHUrl:        repo.GetSSHURL(),
				DefaultBranch: repo.GetDefaultBranch(),
				Archived:      repo.GetArchived(),
			})
		}

		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	return allRepos, nil
}

func (c *Client) ListOrgRepos(org string) ([]Repository, error) {
	var allRepos []Repository
	opts := &github.RepositoryListByOrgOptions{
		ListOptions: github.ListOptions{PerPage: 100},
	}

	for {
		repos, resp, err := c.client.Repositories.ListByOrg(c.ctx, org, opts)
		if err != nil {
			return nil, err
		}

		for _, repo := range repos {
			allRepos = append(allRepos, Repository{
				Owner:         repo.GetOwner().GetLogin(),
				Name:          repo.GetName(),
				CloneURL:      repo.GetCloneURL(),
				SSHUrl:        repo.GetSSHURL(),
				DefaultBranch: repo.GetDefaultBranch(),
				Archived:      repo.GetArchived(),
			})
		}

		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	return allRepos, nil
}

func (c *Client) ListBranches(owner, repo string) ([]Branch, error) {
	var allBranches []Branch
	opts := &github.BranchListOptions{
		ListOptions: github.ListOptions{PerPage: 100},
	}

	for {
		branches, resp, err := c.client.Repositories.ListBranches(c.ctx, owner, repo, opts)
		if err != nil {
			return nil, err
		}

		for _, branch := range branches {
			allBranches = append(allBranches, Branch{Name: branch.GetName()})
		}

		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	return allBranches, nil
}

func (c *Client) GetAuthenticatedUser() (string, error) {
	user, _, err := c.client.Users.Get(c.ctx, "")
	if err != nil {
		return "", err
	}
	return user.GetLogin(), nil
}

func InsertTokenInURL(cloneURL, token string) string {
	u, err := url.Parse(cloneURL)
	if err != nil {
		return cloneURL
	}
	u.User = url.UserPassword("x-access-token", token)
	return u.String()
}
