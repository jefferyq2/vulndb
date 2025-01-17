// Copyright 2022 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cve5

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"golang.org/x/vulndb/internal/derrors"
	"golang.org/x/vulndb/internal/report"
	"golang.org/x/vulndb/internal/stdlib"
	"golang.org/x/vulndb/internal/version"
)

var (
	// The universal unique identifier for the Go Project CNA, which
	// needs to be included CVE JSON 5.0 records.
	GoOrgUUID = "1bb62c36-49e3-4200-9d77-64a1400537cc"
)

// FromReport creates a CVE in 5.0 format from a YAML report file.
func FromReport(r *report.Report) (_ *CVERecord, err error) {
	defer derrors.Wrap(&err, "FromReport(%q)", r.ID)

	if r.CVEMetadata == nil {
		return nil, errors.New("report missing cve_metadata section")
	}
	if r.CVEMetadata.ID == "" {
		return nil, errors.New("report missing CVE ID")
	}
	description := r.CVEMetadata.Description
	if description == "" {
		description = r.Description.String()
	}
	if r.CVEMetadata.CWE == "" {
		return nil, errors.New("report missing CWE")
	}

	c := &CNAPublishedContainer{
		ProviderMetadata: ProviderMetadata{
			OrgID: GoOrgUUID,
		},
		Title: report.RemoveNewlines(r.Summary.String()),
		Descriptions: []Description{
			{
				Lang:  "en",
				Value: report.RemoveNewlines(description),
			},
		},
		ProblemTypes: []ProblemType{
			{
				Descriptions: []ProblemTypeDescription{
					{
						Lang:        "en",
						Description: r.CVEMetadata.CWE,
					},
				},
			},
		},
	}

	for _, m := range r.Modules {
		versions, defaultStatus := versionRangeToVersionRange(m.Versions)
		for _, p := range m.Packages {
			affected := Affected{
				Vendor:        report.Vendor(m.Module),
				Product:       p.Package,
				CollectionURL: "https://pkg.go.dev",
				PackageName:   p.Package,
				Versions:      versions,
				DefaultStatus: defaultStatus,
				Platforms:     p.GOOS,
			}
			for _, symbol := range p.AllSymbols() {
				affected.ProgramRoutines = append(affected.ProgramRoutines, ProgramRoutine{Name: symbol})
			}
			c.Affected = append(c.Affected, affected)
		}
	}

	for _, ref := range r.References {
		c.References = append(c.References, Reference{URL: ref.URL})
	}
	c.References = append(c.References, Reference{
		URL: report.GoAdvisory(r.ID),
	})
	for _, ref := range r.CVEMetadata.References {
		c.References = append(c.References, Reference{URL: ref})
	}

	for _, credit := range r.Credits {
		c.Credits = append(c.Credits, Credit{
			Lang:  "en",
			Value: credit,
		})
	}

	return &CVERecord{
		DataType:    "CVE_RECORD",
		DataVersion: "5.0",
		Metadata: Metadata{
			ID: r.CVEMetadata.ID,
		},
		Containers: Containers{
			CNAContainer: *c,
		},
	}, nil
}

const (
	typeSemver  = "semver"
	versionZero = "0"
)

func versionRangeToVersionRange(versions []report.VersionRange) ([]VersionRange, VersionStatus) {
	if len(versions) == 0 {
		// If there are no recorded versions affected, we assume all versions are affected.
		return nil, StatusAffected
	}

	var cveVRs []VersionRange

	// If there is no final fixed version, then the default status is
	// "affected" and we express the versions in terms of which ranges
	// are *unaffected*. This is due to the fact that the CVE schema
	// does not allow us to express a range as "version X.X.X and above are affected".
	if versions[len(versions)-1].Fixed == "" {
		current := &VersionRange{}
		for _, vr := range versions {
			if vr.Introduced != "" {
				if current.Introduced == "" {
					current.Introduced = versionZero
				}
				current.Fixed = Version(vr.Introduced)
				current.Status = StatusUnaffected
				current.VersionType = typeSemver
				cveVRs = append(cveVRs, *current)
				current = &VersionRange{}
			}
			if vr.Fixed != "" {
				current.Introduced = Version(vr.Fixed)
			}
		}
		return cveVRs, StatusAffected
	}

	// Otherwise, express the version ranges normally as affected ranges,
	// with a default status of "unaffected".
	for _, vr := range versions {
		cveVR := VersionRange{
			Status:      StatusAffected,
			VersionType: typeSemver,
		}
		if vr.Introduced != "" {
			cveVR.Introduced = Version(vr.Introduced)
		} else {
			cveVR.Introduced = versionZero
		}
		if vr.Fixed != "" {
			cveVR.Fixed = Version(vr.Fixed)
		}
		cveVRs = append(cveVRs, cveVR)
	}

	return cveVRs, StatusUnaffected
}

var _ report.Source = &CVERecord{}

func (c *CVERecord) ToReport(modulePath string) *report.Report {
	return cve5ToReport(c, modulePath)
}

func (c *CVERecord) SourceID() string {
	return c.Metadata.ID
}

func cve5ToReport(c *CVERecord, modulePath string) *report.Report {
	cna := c.Containers.CNAContainer

	var description report.Description
	for _, d := range cna.Descriptions {
		if d.Lang == "en" {
			description += report.Description(d.Value + "\n")
		}
	}

	var credits []string
	for _, c := range cna.Credits {
		credits = append(credits, c.Value)
	}

	var refs []*report.Reference
	for _, ref := range c.Containers.CNAContainer.References {
		refs = append(refs, report.ReferenceFromUrl(ref.URL))
	}

	r := &report.Report{
		Modules:     affectedToModules(cna.Affected, modulePath),
		Summary:     report.Summary(cna.Title),
		Description: description,
		Credits:     credits,
		References:  refs,
	}

	r.AddCVE(c.Metadata.ID, getCWE5(&cna), isGoCNA5(&cna))
	return r
}

func getCWE5(c *CNAPublishedContainer) string {
	if len(c.ProblemTypes) == 0 || len(c.ProblemTypes[0].Descriptions) == 0 {
		return ""
	}
	return c.ProblemTypes[0].Descriptions[0].Description
}

func isGoCNA5(c *CNAPublishedContainer) bool {
	return c.ProviderMetadata.OrgID == GoOrgUUID
}

func affectedToModules(as []Affected, modulePath string) []*report.Module {
	// Use a placeholder module if there is no information on
	// modules/packages in the CVE.
	if len(as) == 0 {
		return []*report.Module{{
			Module: modulePath,
		}}
	}

	var modules []*report.Module
	for _, a := range as {
		modules = append(modules, affectedToModule(&a, modulePath))
	}

	return modules
}

func affectedToModule(a *Affected, modulePath string) *report.Module {
	var pkgPath string
	isSet := func(s string) bool {
		const na = "n/a"
		return s != "" && s != na
	}
	switch {
	case isSet(a.PackageName):
		pkgPath = a.PackageName
	case isSet(a.Product):
		pkgPath = a.Product
	case isSet(a.Vendor):
		pkgPath = a.Vendor
	default:
		pkgPath = modulePath
	}

	// If the package path is just a suffix of the modulePath,
	// it is probably not useful.
	if strings.HasSuffix(modulePath, pkgPath) {
		pkgPath = modulePath
	}

	if stdlib.Contains(pkgPath) {
		if strings.HasPrefix(pkgPath, stdlib.ToolchainModulePath) {
			modulePath = stdlib.ToolchainModulePath
		} else {
			modulePath = stdlib.ModulePath
		}
	}

	var symbols []string
	for _, s := range a.ProgramRoutines {
		symbols = append(symbols, s.Name)
	}

	vs, uvs := convertVersions(a.Versions, a.DefaultStatus)

	return &report.Module{
		Module:              modulePath,
		Versions:            vs,
		UnsupportedVersions: uvs,
		Packages: []*report.Package{
			{
				Package: pkgPath,
				Symbols: symbols,
				GOOS:    a.Platforms,
			},
		},
	}
}

func convertVersions(vrs []VersionRange, defaultStatus VersionStatus) (vs []report.VersionRange, uvs []report.UnsupportedVersion) {
	for _, vr := range vrs {
		// Version ranges starting with "n/a" don't have any meaningful data.
		if vr.Introduced == "n/a" {
			continue
		}
		v, ok := toVersionRange(&vr, defaultStatus)
		if ok {
			vs = append(vs, *v)
			continue
		}
		uvs = append(uvs, toUnsupported(&vr, defaultStatus))
	}
	return vs, uvs
}

var (
	// Regex for matching version strings like "<= X, < Y".
	introducedFixedRE = regexp.MustCompile(`^>= (.+), < (.+)$`)
	// Regex for matching version strings like "< Y".
	fixedRE = regexp.MustCompile(`^< (.+)$`)
)

func toVersionRange(cvr *VersionRange, defaultStatus VersionStatus) (*report.VersionRange, bool) {
	// Handle special cases where the info is not quite correctly encoded but
	// we can still figure out the intent.

	// Case one: introduced version is of the form "<= X, < Y".
	if m := introducedFixedRE.FindStringSubmatch(string(cvr.Introduced)); len(m) == 3 {
		return &report.VersionRange{
			Introduced: m[1],
			Fixed:      m[2],
		}, true
	}

	// Case two: introduced version is of the form "< Y".
	if m := fixedRE.FindStringSubmatch(string(cvr.Introduced)); len(m) == 2 {
		return &report.VersionRange{
			Fixed: m[1],
		}, true
	}

	// For now, don't attempt to fix any other messed up cases.
	if cvr.VersionType != typeSemver ||
		cvr.LessThanOrEqual != "" ||
		!version.IsValid(string(cvr.Introduced)) ||
		!version.IsValid(string(cvr.Fixed)) ||
		cvr.Status != StatusAffected ||
		defaultStatus != StatusUnaffected {
		return nil, false
	}

	introduced := string(cvr.Introduced)
	if introduced == "0" {
		introduced = ""
	}

	return &report.VersionRange{
		Introduced: introduced,
		Fixed:      string(cvr.Fixed),
	}, true
}

func toUnsupported(cvr *VersionRange, defaultStatus VersionStatus) report.UnsupportedVersion {
	var version string
	switch {
	case cvr.Fixed != "":
		version = fmt.Sprintf("%s from %s before %s", cvr.Status, cvr.Introduced, cvr.Fixed)
	case cvr.LessThanOrEqual != "":
		version = fmt.Sprintf("%s from %s to %s", cvr.Status, cvr.Introduced, cvr.Fixed)
	default:
		version = fmt.Sprintf("%s at %s", cvr.Status, cvr.Introduced)
	}
	if defaultStatus != "" {
		version = fmt.Sprintf("%s (default: %s)", version, defaultStatus)
	}
	return report.UnsupportedVersion{
		Version: version,
		Type:    "cve_version_range",
	}
}
