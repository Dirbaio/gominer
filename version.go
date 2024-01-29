/*
 * Copyright (c) 2013, 2014 The btcsuite developers
 * Copyright (c) 2015-2023 The Decred developers
 * Copyright (c) 2016 Dario Nieuwenhuis
 *
 * Permission to use, copy, modify, and distribute this software for any
 * purpose with or without fee is hereby granted, provided that the above
 * copyright notice and this permission notice appear in all copies.
 *
 * THE SOFTWARE IS PROVIDED "AS IS" AND THE AUTHOR DISCLAIMS ALL WARRANTIES
 * WITH REGARD TO THIS SOFTWARE INCLUDING ALL IMPLIED WARRANTIES OF
 * MERCHANTABILITY AND FITNESS. IN NO EVENT SHALL THE AUTHOR BE LIABLE FOR
 * ANY SPECIAL, DIRECT, INDIRECT, OR CONSEQUENTIAL DAMAGES OR ANY DAMAGES
 * WHATSOEVER RESULTING FROM LOSS OF USE, DATA OR PROFITS, WHETHER IN AN
 * ACTION OF CONTRACT, NEGLIGENCE OR OTHER TORTIOUS ACTION, ARISING OUT OF
 * OR IN CONNECTION WITH THE USE OR PERFORMANCE OF THIS SOFTWARE.
 */

package main

import (
	"fmt"
	"regexp"
	"runtime/debug"
	"strconv"
	"strings"
)

const (
	// semanticAlphabet defines the allowed characters for the pre-release and
	// build metadata portions of a semantic version string.
	semanticAlphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz-."
)

// semverRE is a regular expression used to parse a semantic version string into
// its constituent parts.
var semverRE = regexp.MustCompile(`^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)` +
	`(?:-((?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*)(?:\.(?:0|[1-9]\d*|\d*` +
	`[a-zA-Z-][0-9a-zA-Z-]*))*))?(?:\+([0-9a-zA-Z-]+(?:\.[0-9a-zA-Z-]+)*))?$`)

// These variables define the application version and follow the semantic
// versioning 2.0.0 spec (https://semver.org/).
var (
	// Note for maintainers:
	//
	// The expected process for setting the version in releases is as follows:
	// - Create a release branch of the form 'release-vMAJOR.MINOR'
	// - Modify the Version variable below on that branch to:
	//   - Remove the pre-release portion
	//   - Set the build metadata to 'release.local'
	// - Update the Version variable below on the master branch to the next
	//   expected version while retaining a pre-release of 'pre'
	//
	// These steps ensure that building from source produces versions that are
	// distinct from reproducible builds that override the Version via linker
	// flags.

	// Version is the application version per the semantic versioning 2.0.0 spec
	// (https://semver.org/).
	//
	// It is defined as a variable so it can be overridden during the build
	// process with:
	// '-ldflags "-X main.Version=fullsemver"'
	// if needed.
	//
	// It MUST be a full semantic version per the semantic versioning spec or
	// the app will panic at runtime.  Of particular note is the pre-release
	// and build metadata portions MUST only contain characters from
	// semanticAlphabet.
	Version = "2.1.0-pre"

	// NOTE: The following values are set via init by parsing the above Version
	// string.

	// These fields are the individual semantic version components that define
	// the application version.
	Major         uint32
	Minor         uint32
	Patch         uint32
	PreRelease    string
	BuildMetadata string
)

// parseUint32 converts the passed string to an unsigned integer or returns an
// error if it is invalid.
func parseUint32(s string, fieldName string) (uint32, error) {
	val, err := strconv.ParseUint(s, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("malformed semver %s: %w", fieldName, err)
	}
	return uint32(val), err
}

// checkSemString returns an error if the passed string contains characters that
// are not in the provided alphabet.
func checkSemString(s, alphabet, fieldName string) error {
	for _, r := range s {
		if !strings.ContainsRune(alphabet, r) {
			return fmt.Errorf("malformed semver %s: %q invalid", fieldName, r)
		}
	}
	return nil
}

// parseSemVer parses various semver components from the provided string.
func parseSemVer(s string) (uint32, uint32, uint32, string, string, error) {
	// Parse the various semver component from the version string via a regular
	// expression.
	m := semverRE.FindStringSubmatch(s)
	if m == nil {
		err := fmt.Errorf("malformed version string %q: does not conform to "+
			"semver specification", s)
		return 0, 0, 0, "", "", err
	}

	major, err := parseUint32(m[1], "major")
	if err != nil {
		return 0, 0, 0, "", "", err
	}

	minor, err := parseUint32(m[2], "minor")
	if err != nil {
		return 0, 0, 0, "", "", err
	}

	patch, err := parseUint32(m[3], "patch")
	if err != nil {
		return 0, 0, 0, "", "", err
	}

	preRel := m[4]
	err = checkSemString(preRel, semanticAlphabet, "pre-release")
	if err != nil {
		return 0, 0, 0, "", "", err
	}

	build := m[5]
	err = checkSemString(build, semanticAlphabet, "buildmetadata")
	if err != nil {
		return 0, 0, 0, "", "", err
	}

	return major, minor, patch, preRel, build, nil
}

// vcsCommitID attempts to return the version control system short commit hash
// that was used to build the binary.  It currently only detects git commits.
func vcsCommitID() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	var vcs, revision string
	for _, bs := range bi.Settings {
		switch bs.Key {
		case "vcs":
			vcs = bs.Value
		case "vcs.revision":
			revision = bs.Value
		}
	}
	if vcs == "" {
		return ""
	}
	if vcs == "git" && len(revision) > 9 {
		revision = revision[:9]
	}
	return revision
}

func init() {
	var err error
	Major, Minor, Patch, PreRelease, BuildMetadata, err = parseSemVer(Version)
	if err != nil {
		panic(err)
	}
	if BuildMetadata == "" {
		BuildMetadata = vcsCommitID()
		if BuildMetadata != "" {
			Version = fmt.Sprintf("%d.%d.%d", Major, Minor, Patch)
			if PreRelease != "" {
				Version += "-" + PreRelease
			}
			Version += "+" + BuildMetadata
		}
	}
}
