// What kind of file a name is - for the file viewer (kept apart from the editor, which loads only when a file opens).

/** What a file is, for colours and the viewer's buttons. */
export type FileKind = 'json' | 'yaml' | 'xml' | 'yang' | 'log' | 'config' | 'csv' | 'ini' | 'shell' | 'code' | 'text';

/** What kind of file a name is (for colours and buttons); '' when the viewer should not open it. */
export function kindOf(name: string): FileKind | '' {
  const n = name.toLowerCase(), ext = n.includes('.') ? n.slice(n.lastIndexOf('.') + 1) : '';
  if (/\.(log|out)(\.\d+)?$/.test(n) || /^(syslog|messages|dmesg)(\.\d+)?$/.test(n)) return 'log';
  if (ext === 'json' || ext === 'geojson' || n.endsWith('.json.txt')) return 'json';
  if (ext === 'yaml' || ext === 'yml') return 'yaml';
  if (['xml', 'xsd', 'xsl', 'svg', 'html', 'htm', 'plist', 'xhtml'].includes(ext)) return 'xml';
  if (ext === 'yang') return 'yang';
  if (['cfg', 'conf', 'config', 'cnf', 'xsf', 'pol'].includes(ext) || /running-config|startup-config/.test(n)) return 'config';
  if (ext === 'csv' || ext === 'tsv') return 'csv';
  if (['ini', 'properties', 'env', 'toml'].includes(ext) || n === '.env') return 'ini';
  if (['sh', 'bash', 'zsh', 'ksh'].includes(ext) || /^\.(bash|zsh)rc$|^\.profile$/.test(n)) return 'shell';
  if (['go', 'py', 'js', 'mjs', 'ts', 'tcl', 'exp', 'c', 'h', 'cpp', 'java', 'rs', 'rb', 'pl', 'lua', 'ps1', 'bat', 'cmd'].includes(ext)) return 'code';
  if (['txt', 'md', 'text', 'rst', 'csv', 'diff', 'patch', 'pem', 'crt', 'pub', 'key', 'cer', 'gitignore', 'mk'].includes(ext) || !ext || /^(makefile|dockerfile|readme|license|hosts|authorized_keys|known_hosts)$/.test(n)) return 'text';
  return '';
}

