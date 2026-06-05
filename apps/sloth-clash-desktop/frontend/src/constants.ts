export const LS_THEME = 'sloth-theme'
export const LS_SPOTLIGHT = 'sloth-spotlight-tour-v2'
export const LS_NAV_COLLAPSED = 'sloth-nav-collapsed-v1'
export const LS_SETTINGS = 'sloth-settings-v1'

export const APP_REPO_URL = 'https://github.com/Nemu-x/SlothClash'

// Full mihomo (Clash.Meta) rule-type set, grouped: domain / IP / source-IP /
// port / process / inbound / misc / rule-set & logical / MATCH last.
export const RULE_TYPE_OPTIONS = [
  // domain
  'DOMAIN-SUFFIX',
  'DOMAIN',
  'DOMAIN-KEYWORD',
  'DOMAIN-REGEX',
  'GEOSITE',
  // destination IP
  'IP-CIDR',
  'IP-CIDR6',
  'IP-SUFFIX',
  'IP-ASN',
  'GEOIP',
  // source IP
  'SRC-IP-CIDR',
  'SRC-IP-SUFFIX',
  'SRC-GEOIP',
  'SRC-IP-ASN',
  // port
  'DST-PORT',
  'SRC-PORT',
  'IN-PORT',
  // process
  'PROCESS-NAME',
  'PROCESS-PATH',
  'PROCESS-NAME-REGEX',
  'PROCESS-PATH-REGEX',
  // inbound / user
  'IN-TYPE',
  'IN-USER',
  'IN-NAME',
  'UID',
  // misc
  'NETWORK',
  'DSCP',
  // rule providers & logical
  'RULE-SET',
  'AND',
  'OR',
  'NOT',
  'SUB-RULE',
  // catch-all
  'MATCH',
] as const
