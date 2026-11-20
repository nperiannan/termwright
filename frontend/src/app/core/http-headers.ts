/** Request headers the API client offers, with values people usually give them. */
export const HTTP_HEADERS: { key: string; desc: string; values: string[] }[] = [
  { key: 'Accept', desc: 'what the answer may be', values: ['application/json', 'application/yang-data+json', 'application/yang-data+xml', 'application/xml', 'text/plain', 'text/html', 'application/octet-stream', '*/*'] },
  { key: 'Content-Type', desc: 'what the body is', values: ['application/json', 'application/yang-data+json', 'application/yang-data+xml', 'application/xml', 'application/x-www-form-urlencoded', 'multipart/form-data', 'text/plain', 'application/octet-stream', 'application/merge-patch+json', 'application/json-patch+json'] },
  { key: 'Authorization', desc: 'credentials', values: ['Bearer {{token}}', 'Basic {{basic}}', 'Token {{token}}', 'ApiKey {{apiKey}}'] },
  { key: 'Cache-Control', desc: 'caching', values: ['no-cache', 'no-store', 'max-age=0'] },
  { key: 'Accept-Encoding', desc: 'compression', values: ['gzip, deflate, br', 'gzip', 'identity'] },
  { key: 'Accept-Language', desc: 'language of the answer', values: ['en-US', 'en', 'de-DE', 'fr-FR', 'ja-JP', 'ru-RU'] },
  { key: 'Accept-Charset', desc: 'character set', values: ['utf-8'] },
  { key: 'Connection', desc: 'keep the connection', values: ['keep-alive', 'close'] },
  { key: 'X-Request-Id', desc: 'trace a request', values: ['{{$guid}}'] },
  { key: 'X-Correlation-Id', desc: 'trace a call chain', values: ['{{$guid}}'] },
  { key: 'X-Auth-Token', desc: 'device token', values: ['{{token}}'] },
  { key: 'X-API-Key', desc: 'an API key', values: ['{{apiKey}}'] },
  { key: 'X-CSRF-Token', desc: 'CSRF protection', values: ['{{csrf}}'] },
  { key: 'X-HTTP-Method-Override', desc: 'method through a proxy', values: ['PATCH', 'PUT', 'DELETE'] },
  { key: 'X-Forwarded-For', desc: 'client address', values: ['{{clientIp}}'] },
  { key: 'If-Match', desc: 'only if not changed (ETag)', values: ['"{{etag}}"', '*'] },
  { key: 'If-None-Match', desc: 'only if changed (ETag)', values: ['"{{etag}}"', '*'] },
  { key: 'If-Modified-Since', desc: 'only if newer', values: ['{{$isoTimestamp}}'] },
  { key: 'If-Unmodified-Since', desc: 'only if not newer', values: ['{{$isoTimestamp}}'] },
  { key: 'Prefer', desc: 'server preferences', values: ['return=representation', 'return=minimal', 'respond-async'] },
  { key: 'Origin', desc: 'CORS origin', values: ['https://{{host}}'] },
  { key: 'Referer', desc: 'page it came from', values: ['https://{{host}}/'] },
  { key: 'User-Agent', desc: 'client name', values: ['Termwright', 'curl/8.0', 'Mozilla/5.0'] },
  { key: 'Cookie', desc: 'cookies', values: ['name=value'] },
  { key: 'Range', desc: 'part of the answer', values: ['bytes=0-1023'] },
  { key: 'Host', desc: 'server name', values: ['{{host}}'] },
  { key: 'Expect', desc: 'ask before a large body', values: ['100-continue'] },
  { key: 'Pragma', desc: 'old caching', values: ['no-cache'] },
  { key: 'DNT', desc: 'do not track', values: ['1'] },
  { key: 'Upgrade-Insecure-Requests', desc: 'prefer HTTPS', values: ['1'] },
];

/** Values for a header that is not in the list: none. */
export function headerValues(key: string): string[] {
  return HTTP_HEADERS.find(h => h.key.toLowerCase() === key.trim().toLowerCase())?.values ?? [];
}
