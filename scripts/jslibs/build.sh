#!/bin/sh
# Bundles Postman's sandbox libraries for the API client's scripts into internal/apiclient/jslibs/*.js.gz.
# Run here after `npm ci`. lodash and moment are their own minified files; the others are esbuild bundles
# (IIFE, ES2015) that leave their export in __lib and share one Buffer (globalThis.Buffer).
set -e
cd "$(dirname "$0")"
OUT=../../internal/apiclient/jslibs
mkdir -p build
opts="--bundle --format=iife --global-name=__lib --platform=browser --target=es2015 --minify --legal-comments=none --log-level=warning"
mk() {
  echo "module.exports = require('$2');" > build/e-$1.js
  npx esbuild build/e-$1.js $opts --alias:stream=stream-browserify --alias:timers=./shims/timers.js --alias:util=util --alias:events=events \
    --alias:buffer=./shims/buffer-global.js --define:Buffer=globalThis.Buffer --alias:string_decoder=string_decoder --alias:path=path-browserify \
    --alias:url=url --alias:querystring=querystring-es3 --alias:punycode=punycode --inject:./shims/inject.js --outfile=build/$1.js
}
echo "module.exports = require('buffer');" > build/e-buffer.js
npx esbuild build/e-buffer.js $opts --outfile=build/buffer.js
for p in cheerio:cheerio xml2js:xml2js crypto-js:crypto-js tv4:tv4 chai:chai ajv:ajv csv-parse:csv-parse/lib/sync uuid:uuid path:path-browserify \
  url:url querystring:querystring-es3 util:util events:events assert:assert punycode:punycode string_decoder:string_decoder stream:stream-browserify \
  postman-collection:postman-collection; do
  mk "${p%%:*}" "${p#*:}"
done
sed '/sourceMappingURL/d' node_modules/lodash/lodash.min.js > build/lodash.js
sed '/sourceMappingURL/d' node_modules/moment/min/moment.min.js > build/moment.js
for f in build/*.js; do case $f in build/e-*) ;; *) gzip -9 -n -c "$f" > "$OUT/$(basename "$f").gz" ;; esac; done
echo "done: $(ls $OUT/*.js.gz | wc -l) libraries in $OUT (update $OUT/LICENSES.txt when versions change)"
