package handler

import (
	"strings"

	"github.com/gin-gonic/gin"
)

const (
	deploymentBasePathPlaceholder = "__POETRY_DEPLOYMENT_BASE_PATH__"
	apiBasePathPlaceholder        = "__POETRY_API_BASE_PATH__"
)

// forwardedPrefix returns a conservative, path-only deployment prefix supplied
// by the reverse proxy. Invalid values deliberately fall back to root hosting
// so a client-provided header can never be reflected into HTML, JS, or YAML.
func forwardedPrefix(c *gin.Context) string {
	prefix := strings.TrimSpace(c.GetHeader("X-Forwarded-Prefix"))
	if prefix == "" || prefix == "/" {
		return ""
	}

	prefix = strings.TrimRight(prefix, "/")
	if prefix == "" || !strings.HasPrefix(prefix, "/") {
		return ""
	}

	for _, segment := range strings.Split(strings.TrimPrefix(prefix, "/"), "/") {
		if segment == "" || segment == "." || segment == ".." {
			return ""
		}
		for _, character := range segment {
			if !isForwardedPrefixCharacter(character) {
				return ""
			}
		}
	}

	return prefix
}

func isForwardedPrefixCharacter(character rune) bool {
	return character >= 'a' && character <= 'z' ||
		character >= 'A' && character <= 'Z' ||
		character >= '0' && character <= '9' ||
		strings.ContainsRune("-._~", character)
}

func externalPath(prefix, rootedPath string) string {
	if prefix == "" || rootedPath == "" || !strings.HasPrefix(rootedPath, "/") {
		return rootedPath
	}
	return prefix + rootedPath
}

// renderProductContent adapts the built-in product assets for a trusted
// reverse-proxy prefix while preserving the root-hosted development contract.
func renderProductContent(c *gin.Context, content string) []byte {
	prefix := forwardedPrefix(c)
	deploymentBasePath := prefix
	if deploymentBasePath == "" {
		deploymentBasePath = "/"
	}

	content = strings.ReplaceAll(content, deploymentBasePathPlaceholder, deploymentBasePath)
	content = strings.ReplaceAll(content, apiBasePathPlaceholder, externalPath(prefix, "/api/v1"))
	if prefix == "" {
		return []byte(content)
	}

	return []byte(prefixProductRootPaths(content, prefix))
}

// renderProductHTML additionally protects links assembled by client-side code
// from API payloads (for example media and certificate URLs). Static links are
// still rewritten on the server; the small bridge only handles later DOM writes.
func renderProductHTML(c *gin.Context, content string) []byte {
	rendered := string(renderProductContent(c, content))
	prefix := forwardedPrefix(c)
	if prefix == "" {
		return []byte(rendered)
	}

	return []byte(strings.Replace(rendered, "</head>", prefixRuntimeBridge(prefix)+"</head>", 1))
}

func prefixRuntimeBridge(prefix string) string {
	return `<script data-poetry-prefix-bridge>(function(){const base="` + prefix + `",roots=["/api/v1","/console","/home","/docs","/pricing","/manifest.json","/service-worker.js","/pwa-icon.svg","/console-placeholder-bg.png","/library","/u/","/users/","/certificates/","/media-assets/","/openapi.yaml","/favicon.ico"];function productPath(path){return roots.some(function(root){return path===root||path.indexOf(root+"/")===0||path.indexOf(root+"?")===0||path.indexOf(root+"#")===0||(root.charAt(root.length-1)==="/"&&path.indexOf(root)===0)})}function external(path){if(typeof path!=="string"||path.charAt(0)!=="/"||path.indexOf("//")===0||path===base||path.indexOf(base+"/")===0||!productPath(path))return path;return base+path}function rewriteElement(element){if(!element||element.nodeType!==1)return;["href","src","action"].forEach(function(attribute){const value=element.getAttribute(attribute);const next=external(value);if(next!==value)element.setAttribute(attribute,next)})}function rewriteTree(root){rewriteElement(root);if(root&&root.querySelectorAll)root.querySelectorAll("[href],[src],[action]").forEach(rewriteElement)}function observe(){rewriteTree(document);new MutationObserver(function(records){records.forEach(function(record){if(record.type==="attributes")rewriteElement(record.target);record.addedNodes.forEach(rewriteTree)})}).observe(document.documentElement,{attributes:true,attributeFilter:["href","src","action"],childList:true,subtree:true})}if(document.readyState==="loading")document.addEventListener("DOMContentLoaded",observe,{once:true});else observe()})();</script>`
}

func prefixProductRootPaths(content, prefix string) string {
	// Only rewrite known product routes. Rewriting every quoted slash would also
	// mutate HTML fragments such as "</div>" inside inline JavaScript.
	for _, rootedPath := range []string{
		"/api/v1",
		"/console",
		"/home",
		"/docs",
		"/pricing",
		"/manifest.json",
		"/service-worker.js",
		"/pwa-icon.svg",
		"/console-placeholder-bg.png",
		"/library",
		"/u/",
		"/users/",
		"/certificates/",
		"/media-assets/",
		"/openapi.yaml",
		"/favicon.ico",
	} {
		content = strings.ReplaceAll(content, `"`+rootedPath, `"`+prefix+rootedPath)
		content = strings.ReplaceAll(content, `'`+rootedPath, `'`+prefix+rootedPath)
	}

	// Root is handled only in URL-bearing contexts. A blanket rewrite of the
	// string literal "/" would break client-side path parsing such as split("/").
	content = strings.ReplaceAll(content, `href="/"`, `href="`+prefix+`/"`)
	content = strings.ReplaceAll(content, `href='/'`, `href='`+prefix+`/'`)
	content = strings.ReplaceAll(content, `"scope": "/"`, `"scope": "`+prefix+`/"`)
	content = strings.ReplaceAll(content, `CORE_ASSETS = ["/",`, `CORE_ASSETS = ["`+prefix+`/",`)
	return content
}
