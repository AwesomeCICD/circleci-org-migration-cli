package context

// Group represents a security group returned via the GraphQL API.
// groupType is one of TEAM, ORGANIZATION, or ACCOUNT (enum as returned by the
// server; stored verbatim).
type Group struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	GroupType string `json:"groupType"`
}

// ListOrgGroups returns all groups in an organization via GraphQL.
// These are used to resolve group UUIDs found in group-type restrictions.
//
// Query: query($id: ID!){ organization(id:$id){ groups{ edges{ node{ id name groupType } } } } }
func (c *Client) ListOrgGroups(orgID string) ([]Group, error) {
	const query = `query($id: ID!) {
  organization(id: $id) {
    groups {
      edges {
        node {
          id
          name
          groupType
        }
      }
    }
  }
}`
	var data struct {
		Organization struct {
			Groups struct {
				Edges []struct {
					Node Group `json:"node"`
				} `json:"edges"`
			} `json:"groups"`
		} `json:"organization"`
	}

	vars := map[string]interface{}{"id": orgID}
	if err := doGQL(c.httpClient, c.gqlBaseURL, c.token, query, vars, &data); err != nil {
		return nil, err
	}

	edges := data.Organization.Groups.Edges
	groups := make([]Group, len(edges))
	for i, e := range edges {
		groups[i] = e.Node
	}
	return groups, nil
}

// ListContextGroups returns the groups attached to a specific context via GraphQL.
// This is used to resolve which groups have been granted access to a context.
//
// Query: query($id: ID!){ context(id:$id){ groups{ edges{ node{ id name groupType } } } } }
func (c *Client) ListContextGroups(contextID string) ([]Group, error) {
	const query = `query($id: ID!) {
  context(id: $id) {
    groups {
      edges {
        node {
          id
          name
          groupType
        }
      }
    }
  }
}`
	var data struct {
		Context struct {
			Groups struct {
				Edges []struct {
					Node Group `json:"node"`
				} `json:"edges"`
			} `json:"groups"`
		} `json:"context"`
	}

	vars := map[string]interface{}{"id": contextID}
	if err := doGQL(c.httpClient, c.gqlBaseURL, c.token, query, vars, &data); err != nil {
		return nil, err
	}

	edges := data.Context.Groups.Edges
	groups := make([]Group, len(edges))
	for i, e := range edges {
		groups[i] = e.Node
	}
	return groups, nil
}
