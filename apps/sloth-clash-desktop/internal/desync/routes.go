// Package desync is the DPI-desync catalog shared by the config generation
// (which writes the managed proxies, groups and rules) and the runtime prober
// in main (which picks a working candidate per network): route names, the
// domains each route covers, and the candidate strategies.
package desync

// Route group names, and the key that marks a generated (managed) entry.
const YouTubeRoute = "SLOTH-DESYNC-YOUTUBE"
const DiscordRoute = "SLOTH-DESYNC-DISCORD"
const DiscordUDPRoute = "SLOTH-DESYNC-DISCORD-UDP"
const ManagedKey = "_sloth-desync-managed"

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
