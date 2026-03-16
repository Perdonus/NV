package semver

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var pattern = regexp.MustCompile(`^(?:v)?(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?(?:\+([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?$`)

type Version struct {
	major      int
	minor      int
	patch      int
	prerelease []Identifier
	build      string
}

type Identifier struct {
	raw     string
	numeric bool
	number  int
}

func Parse(raw string) (Version, error) {
	trimmed := strings.TrimSpace(raw)
	matches := pattern.FindStringSubmatch(trimmed)
	if matches == nil {
		return Version{}, fmt.Errorf("invalid semver %q", raw)
	}

	major, err := strconv.Atoi(matches[1])
	if err != nil {
		return Version{}, err
	}
	minor, err := strconv.Atoi(matches[2])
	if err != nil {
		return Version{}, err
	}
	patch, err := strconv.Atoi(matches[3])
	if err != nil {
		return Version{}, err
	}

	prerelease, err := parsePrerelease(matches[4])
	if err != nil {
		return Version{}, err
	}

	return Version{
		major:      major,
		minor:      minor,
		patch:      patch,
		prerelease: prerelease,
		build:      matches[5],
	}, nil
}

func Normalize(raw string) (string, error) {
	version, err := Parse(raw)
	if err != nil {
		return "", err
	}
	return version.String(), nil
}

func Validate(raw string) error {
	_, err := Parse(raw)
	return err
}

func Compare(left, right string) int {
	leftVersion, err := Parse(left)
	if err != nil {
		return 0
	}
	rightVersion, err := Parse(right)
	if err != nil {
		return 0
	}
	return leftVersion.Compare(rightVersion)
}

func (v Version) String() string {
	var builder strings.Builder
	builder.WriteString(strconv.Itoa(v.major))
	builder.WriteByte('.')
	builder.WriteString(strconv.Itoa(v.minor))
	builder.WriteByte('.')
	builder.WriteString(strconv.Itoa(v.patch))
	if len(v.prerelease) > 0 {
		builder.WriteByte('-')
		for index, identifier := range v.prerelease {
			if index > 0 {
				builder.WriteByte('.')
			}
			builder.WriteString(identifier.raw)
		}
	}
	if v.build != "" {
		builder.WriteByte('+')
		builder.WriteString(v.build)
	}
	return builder.String()
}

func (v Version) Compare(other Version) int {
	switch {
	case v.major != other.major:
		return compareInts(v.major, other.major)
	case v.minor != other.minor:
		return compareInts(v.minor, other.minor)
	case v.patch != other.patch:
		return compareInts(v.patch, other.patch)
	}

	if len(v.prerelease) == 0 && len(other.prerelease) == 0 {
		return 0
	}
	if len(v.prerelease) == 0 {
		return 1
	}
	if len(other.prerelease) == 0 {
		return -1
	}

	limit := len(v.prerelease)
	if len(other.prerelease) < limit {
		limit = len(other.prerelease)
	}
	for index := 0; index < limit; index++ {
		cmp := compareIdentifier(v.prerelease[index], other.prerelease[index])
		if cmp != 0 {
			return cmp
		}
	}
	return compareInts(len(v.prerelease), len(other.prerelease))
}

func parsePrerelease(raw string) ([]Identifier, error) {
	if raw == "" {
		return nil, nil
	}

	parts := strings.Split(raw, ".")
	identifiers := make([]Identifier, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			return nil, fmt.Errorf("invalid semver prerelease %q", raw)
		}
		if isNumeric(part) {
			if len(part) > 1 && strings.HasPrefix(part, "0") {
				return nil, fmt.Errorf("invalid semver prerelease identifier %q", part)
			}
			number, err := strconv.Atoi(part)
			if err != nil {
				return nil, err
			}
			identifiers = append(identifiers, Identifier{raw: part, numeric: true, number: number})
			continue
		}
		identifiers = append(identifiers, Identifier{raw: part})
	}
	return identifiers, nil
}

func compareIdentifier(left, right Identifier) int {
	switch {
	case left.numeric && right.numeric:
		return compareInts(left.number, right.number)
	case left.numeric:
		return -1
	case right.numeric:
		return 1
	default:
		return strings.Compare(left.raw, right.raw)
	}
}

func isNumeric(raw string) bool {
	for _, symbol := range raw {
		if symbol < '0' || symbol > '9' {
			return false
		}
	}
	return raw != ""
}

func compareInts(left, right int) int {
	switch {
	case left < right:
		return -1
	case left > right:
		return 1
	default:
		return 0
	}
}
