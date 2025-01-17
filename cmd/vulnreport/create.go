// Copyright 2024 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"flag"
)

var (
	preferCVE       = flag.Bool("cve", false, "for create, prefer CVEs over GHSAs as canonical source")
	graphQL         = flag.Bool("graphql", false, "for create, fetch GHSAs from the Github GraphQL API instead of the OSV database")
	issueRepo       = flag.String("issue-repo", "github.com/golang/vulndb", "for create, repo locate Github issues")
	useAI           = flag.Bool("ai", false, "for create, use AI to write draft summary and description when creating report")
	populateSymbols = flag.Bool("symbols", false, "for create, attempt to auto-populate symbols")
	user            = flag.String("user", "", "for create & create-excluded, only consider issues assigned to the given user")
)

type create struct {
	*issueParser
	*creator
}

func (create) name() string { return "create" }

func (create) usage() (string, string) {
	const desc = "creates a new vulnerability YAML report"
	return ghIssueArgs, desc
}

func (c *create) setup(ctx context.Context) error {
	c.creator = new(creator)
	c.issueParser = new(issueParser)
	return setupAll(ctx, c.creator, c.issueParser)
}

func (c *create) close() error {
	return closeAll(c.issueParser, c.creator)
}

func (c *create) run(ctx context.Context, issueNumber string) (err error) {
	iss, err := c.lookup(ctx, issueNumber)
	if err != nil {
		return err
	}

	if c.skip(iss, c.skipReason) {
		return nil
	}

	return c.reportFromIssue(ctx, iss)
}
