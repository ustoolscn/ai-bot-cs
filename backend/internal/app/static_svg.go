package app

import (
	"golang.org/x/net/html"
	"regexp"
	"strings"
)

const svgNodes = " svg g path rect circle ellipse line polyline polygon text tspan defs linearGradient radialGradient stop title desc "
const svgAttrs = " id class width height x y x1 x2 y1 y2 cx cy r rx ry fx fy fr d points viewBox preserveAspectRatio transform fill fill-rule fill-opacity stroke stroke-width stroke-linecap stroke-linejoin stroke-miterlimit stroke-dasharray stroke-dashoffset stroke-opacity opacity color text-anchor dominant-baseline font-family font-size font-style font-weight dx dy offset stop-color stop-opacity gradientUnits gradientTransform spreadMethod vector-effect "

var svgPaint = regexp.MustCompile(`^url\(#[A-Za-z_][A-Za-z0-9_.:-]*\)$`)
var svgValue = regexp.MustCompile(`^[\pL\pN_#.,%+() /:-]*$`)

func cleanSVG(n *html.Node) {
	attrs := n.Attr[:0]
	for _, a := range n.Attr {
		if a.Namespace != "" || !strings.Contains(svgAttrs, " "+a.Key+" ") {
			continue
		}
		if strings.Contains(strings.ToLower(a.Val), "url") {
			if (a.Key != "fill" && a.Key != "stroke") || !svgPaint.MatchString(a.Val) {
				continue
			}
		} else if !svgValue.MatchString(a.Val) {
			continue
		}
		attrs = append(attrs, a)
	}
	n.Attr = attrs
}

// JSON decoding normally removes escaped delimiters. Repair only residual delimiters
// of attribute values inside tags if a producer escaped its HTML a second time.
var openingTag = regexp.MustCompile(`<[A-Za-z][^<>]*>`)
var escapedAttr = regexp.MustCompile(`(\s[\w:-]+\s*=\s*)\\"([^"<>]*)\\"`)

func normalizeHTMLAttributes(src string) string {
	if !strings.Contains(src, `\"`) {
		return src
	}
	return openingTag.ReplaceAllStringFunc(src, func(tag string) string { return escapedAttr.ReplaceAllString(tag, `${1}"${2}"`) })
}
