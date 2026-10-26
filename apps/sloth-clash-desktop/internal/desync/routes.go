// Package desync is the DPI-desync catalog shared by the config generation
// (which writes the managed proxies, groups and rules) and the runtime prober
// in main (which picks a working candidate per network): route names, the
// domains each route covers, and the candidate strategies.
package desync

import "strings"

// Route group names, and the key that marks a generated (managed) entry.
const YouTubeRoute = "SLOTH-DESYNC-YOUTUBE"
const DiscordRoute = "SLOTH-DESYNC-DISCORD"
const DiscordUDPRoute = "SLOTH-DESYNC-DISCORD-UDP"
const ManagedKey = "_sloth-desync-managed"

// CustomRoutePrefix starts the hidden group name of a custom-domain entry; the
// rest is the entry's stable ID (see CustomRoute).
const CustomRoutePrefix = "SLOTH-DESYNC-CUSTOM-"

// Domains routed through each route.
var YouTubeDomains = []string{"youtube.com", "youtu.be", "googlevideo.com", "ytimg.com", "youtubei.googleapis.com", "youtube.googleapis.com", "yt3.ggpht.com"}
var DiscordDomains = []string{"discord.com", "discord.gg", "discordapp.com", "discordapp.net", "discord.media", "discordcdn.com"}

// Rules returns the managed rules sending domains through route.
func Rules(domains []string, route string) []string {
	var rules []string
	for _, domain := range domains {
		rules = append(rules, "AND,((NETWORK,udp),(DST-PORT,443),(DOMAIN-SUFFIX,"+domain+")),"+route)
		rules = append(rules, "AND,((NETWORK,tcp),(DOMAIN-SUFFIX,"+domain+")),"+route)
	}
	return rules
}

// DiscordUDPRules returns the managed rule for Discord voice (UDP).
func DiscordUDPRules() []string {
	return []string{"AND,((NETWORK,udp),(PROCESS-NAME,Discord.exe),(DST-PORT,1024-65535))," + DiscordUDPRoute}
}

// CustomRoute is the hidden group of the custom entry with the given ID. The
// name follows the entry, not its list position, so removing or reordering
// entries never moves another entry's stored selection.
func CustomRoute(id string) string { return CustomRoutePrefix + id }

// CustomService is the cache/service key of a custom domain.
func CustomService(domain string) string { return "custom:" + domain }

// CustomServiceDomain returns the domain of a CustomService key.
func CustomServiceDomain(service string) (string, bool) {
	return strings.CutPrefix(service, "custom:")
}

// BuiltinDomainFor returns the built-in service ("youtube" or "discord") whose
// domain list already covers domain (equal to, or a subdomain of, one of its
// entries), or "".
func BuiltinDomainFor(domain string) string {
	for _, list := range []struct {
		service string
		domains []string
	}{{"youtube", YouTubeDomains}, {"discord", DiscordDomains}} {
		for _, d := range list.domains {
			if domain == d || strings.HasSuffix(domain, "."+d) {
				return list.service
			}
		}
	}
	return ""
}
