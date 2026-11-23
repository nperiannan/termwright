package testrec

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/termwright/termwright/internal/standalone"
	"github.com/termwright/termwright/internal/webrun"
)

// PlaywrightVersion is the @playwright/test a Playwright export uses.
const PlaywrightVersion = webrun.PlaywrightVersion

// pwApp is a web app of a Playwright export (apps.json): no password - it comes from the environment.
type pwApp struct {
	URL         string          `json:"url"`
	User        string          `json:"user,omitempty"`
	UserEnv     string          `json:"userEnv"`
	PasswordEnv string          `json:"passwordEnv"`
	Login       []webrun.Action `json:"login,omitempty"`
	Browser     string          `json:"browser,omitempty"`
	Viewport    string          `json:"viewport,omitempty"`
	Insecure    bool            `json:"insecure,omitempty"`
	URLEnv      string          `json:"urlEnv"`
}

var jsIdent = regexp.MustCompile(`^[A-Za-z_$][\w$]*$`)

// tsStr is a TypeScript string literal.
func tsStr(s string) string {
	b, _ := json.Marshal(s)
	return strings.NewReplacer(" ", ` `, " ", ` `).Replace(string(b))
}

// tsVal is a value with {{variables}} filled at run time (tw.v), or a plain literal.
func tsVal(s string) string {
	if strings.Contains(s, "{{") {
		return "tw.v(" + tsStr(s) + ", v)"
	}
	return tsStr(s)
}

// PlaywrightSpec writes a test as a Playwright Test file (TypeScript): its web steps as Playwright calls,
// the other steps as comments. depth: how many folders down from the suite's root the file is (for the
// helper's import). data: the test data file in the export (data/….csv), "" for none. ok: false when the
// test has no web step.
func PlaywrightSpec(c Case, depth int, data string, apps map[string]*pwApp) (string, bool) {
	var b strings.Builder
	up := strings.Repeat("../", depth)
	if up == "" {
		up = "./"
	}
	fmt.Fprintf(&b, "// %s - written by Termwright from %s; run: npx playwright test\n", c.Name, orStr(c.file, "the test"))
	b.WriteString("import { test, expect, type Page } from '@playwright/test';\n")
	fmt.Fprintf(&b, "import { tw } from '%stw';\n\n", up)
	// file-wide browser settings from the first web app
	for _, st := range c.Steps {
		if st.Web != nil && st.Web.Login != "" {
			if a := apps[st.Web.Login]; a != nil {
				var use []string
				if a.Browser == "firefox" || a.Browser == "webkit" {
					use = append(use, "browserName: "+tsStr(a.Browser))
				}
				if m := regexp.MustCompile(`^(\d+)\s*[x×]\s*(\d+)$`).FindStringSubmatch(a.Viewport); m != nil {
					use = append(use, "viewport: { width: "+m[1]+", height: "+m[2]+" }")
				}
				if a.Insecure {
					use = append(use, "ignoreHTTPSErrors: true")
				}
				if len(use) > 0 {
					fmt.Fprintf(&b, "test.use({ %s });\n\n", strings.Join(use, ", "))
				}
			}
			break
		}
	}
	keys := make([]string, 0, len(c.Vars))
	for k := range c.Vars {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	b.WriteString("// the test's values ({{name}} in the steps); TW_VAR_<name> or a data row changes them\nconst vars: Record<string, string> = {")
	for i, k := range keys {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, "\n  %s: %s", tsStr(k), tsStr(c.Vars[k]))
	}
	if len(keys) > 0 {
		b.WriteString("\n")
	}
	b.WriteString("};\n")
	if data != "" {
		fmt.Fprintf(&b, "const rows = tw.rows(%s); // a run per row\n\n", tsStr(data))
		b.WriteString("for (const [i, row] of rows.entries()) {\n")
		fmt.Fprintf(&b, "  test(%s + (rows.length > 1 ? ' - row ' + (i + 1) : ''), async ({ page, context }) => {\n", tsStr(c.Name))
		b.WriteString("    const v = tw.vars(vars, row);\n")
	} else {
		b.WriteString("\n{\n")
		fmt.Fprintf(&b, "  test(%s, async ({ page, context }) => {\n", tsStr(c.Name))
		b.WriteString("    const v = tw.vars(vars);\n")
	}
	b.WriteString("    let p: Page = page;\n")
	web := false
	in := "    "
	phases := []struct {
		name  string
		steps []Step
	}{{"setup", c.Setup}, {"", c.Steps}}
	for _, ph := range phases {
		for i, st := range ph.steps {
			n := strconv.Itoa(i + 1)
			if ph.name != "" {
				n = ph.name + " " + n
			}
			if st.Web == nil {
				fmt.Fprintf(&b, "\n%s// step %s - not in Playwright: %s\n", in, n, stepWords(st))
				continue
			}
			web = true
			w := st.Web
			app := apps[w.Login]
			label := "web"
			if w.Login != "" {
				label += " on " + w.Login
			}
			fmt.Fprintf(&b, "\n%sawait tw.step(%s, () => p, async () => {\n", in, tsStr("step "+n+" - "+label))
			in2 := in + "  "
			if st.If != "" {
				fmt.Fprintf(&b, "%s// Termwright runs this step only when: %s\n", in2, st.If)
			}
			if app != nil {
				fmt.Fprintf(&b, "%sawait p.goto(tw.url(%s, %s, v));\n", in2, tsStr(w.Login), tsStr(w.URL))
				fmt.Fprintf(&b, "%sawait tw.login(p, %s, v); // when the page asks to log in\n", in2, tsStr(w.Login))
			} else if w.URL != "" {
				fmt.Fprintf(&b, "%sawait p.goto(%s);\n", in2, tsVal(w.URL))
			}
			for _, a := range w.Actions {
				b.WriteString(pwAction(a, in2, w.Login))
			}
			fmt.Fprintf(&b, "%s});\n", in)
		}
	}
	if len(c.Cleanup) > 0 {
		fmt.Fprintf(&b, "\n%s// cleanup - not in Playwright: %d step(s)\n", in, len(c.Cleanup))
	}
	b.WriteString("  });\n}\n")
	return b.String(), web
}

// stepWords: a non-web step in a few words (a comment of the spec).
func stepWords(st Step) string {
	switch {
	case st.Run != nil:
		return fmt.Sprintf("%q on %s", st.Run.Command, st.Run.Session)
	case st.HTTP != nil:
		return st.HTTP.Method + " " + st.HTTP.URL
	case st.Manual != nil:
		return "manual - " + st.Manual.Instruction
	case st.Wait > 0:
		return fmt.Sprintf("wait %g s", st.Wait)
	}
	return "a step of another kind"
}

// pwAction is one recorded action as Playwright calls.
func pwAction(a webrun.Action, in, app string) string {
	val := tsVal(a.Value)
	if strings.Contains(a.Value, "{{password}}") && app != "" {
		val = "tw.password(" + tsStr(app) + ")"
	}
	el := "tw.find(p, " + tsVal(a.Target)
	if len(a.Alt) > 0 {
		alts := make([]string, len(a.Alt))
		for i, x := range a.Alt {
			alts[i] = tsStr(x)
		}
		el += ", [" + strings.Join(alts, ", ") + "]"
	}
	el += ")"
	e := "(await " + el + ")"
	line := func(s string) string { return in + s + "\n" }
	num := regexp.MustCompile(`^[\d.]+$`)
	switch a.Do {
	case "goto":
		return line("await p.goto(" + val + ");")
	case "click":
		return line("await " + e + ".click();")
	case "dblclick":
		return line("await " + e + ".dblclick();")
	case "rightclick":
		return line("await " + e + ".click({ button: 'right' });")
	case "fill":
		return line("await " + e + ".fill(" + val + ");")
	case "type":
		return line("await " + e + ".pressSequentially(" + val + ");")
	case "clear":
		return line("await " + e + ".clear();")
	case "press":
		if a.Target != "" {
			return line("await " + e + ".press(" + val + ");")
		}
		return line("await p.keyboard.press(" + val + ");")
	case "select":
		return line("await tw.select(" + e + ", " + val + ");")
	case "check", "uncheck", "hover", "focus":
		return line("await " + e + "." + a.Do + "();")
	case "scroll":
		if a.Target != "" {
			return line("await " + e + ".scrollIntoViewIfNeeded();")
		}
		return line("await p.mouse.wheel(0, 600);")
	case "upload":
		return line("await " + e + ".setInputFiles(tw.files(" + val + "));")
	case "download":
		return line("await tw.download(p, " + e + ");")
	case "dialog":
		switch a.Value {
		case "dismiss":
			return line("p.on('dialog', d => d.dismiss());")
		case "accept", "":
			return line("p.on('dialog', d => d.accept());")
		}
		return line("p.on('dialog', d => d.accept(" + val + "));")
	case "tab":
		return line("p = await tw.tab(context, p, " + val + ");")
	case "close_tab":
		return line("await p.close(); p = context.pages()[context.pages().length - 1];")
	case "reload":
		return line("await p.reload();")
	case "back":
		return line("await p.goBack();")
	case "wait":
		if a.Target != "" {
			return line("await " + e + ".waitFor({ state: 'visible' });")
		}
		if a.Value != "" && !num.MatchString(a.Value) {
			return line("await p.waitForURL(u => u.toString().includes(" + val + "));")
		}
		s := a.Value
		if s == "" {
			s = "1"
		}
		return line("await p.waitForTimeout(" + s + " * 1000);")
	case "wait_gone":
		return line("await " + e + ".waitFor({ state: 'hidden' });")
	case "expect":
		if a.Target == "" {
			return line("await expect(p.getByText(" + val + ").first()).toBeVisible();")
		}
		if a.Value != "" {
			return line("await expect(" + e + ").toContainText(" + val + ");")
		}
		return line("await expect(" + e + ").toBeVisible();")
	case "expect_not":
		if a.Target == "" {
			return line("await expect(p.getByText(" + val + ")).toHaveCount(0);")
		}
		return line("await expect(tw.loc(p, " + tsVal(a.Target) + ")).toHaveCount(0);")
	case "expect_value":
		return line("await expect(" + e + ").toHaveValue(" + val + ");")
	case "expect_count":
		return line("await expect(tw.loc(p, " + tsVal(a.Target) + ")).toHaveCount(Number(" + val + "));")
	case "expect_checked":
		return line("await expect(" + e + ").toBeChecked();")
	case "expect_enabled":
		return line("await expect(" + e + ").toBeEnabled();")
	case "expect_url":
		return line("await expect(p).toHaveURL(u => u.toString().includes(" + val + "));")
	case "expect_title":
		return line("await expect(p).toHaveTitle(new RegExp(tw.re(" + val + ")));")
	case "read":
		name := orStr(a.Value, a.Target)
		return line("v[" + tsStr(name) + "] = await tw.read(" + e + ");")
	case "screenshot":
		return line("await p.screenshot({ path: test.info().outputPath(" + tsStr(orStr(a.Value, "screenshot")+".png") + ") });")
	}
	return line("// " + webrun.ActionText(a) + " - not available in Playwright Test")
}

// playwrightApp is a web app session as apps.json keeps it (from the inventory: never a password).
func playwrightApp(session string, d standalone.Device) *pwApp {
	return &pwApp{URL: d.WebURL, User: d.Username, UserEnv: standalone.EnvName("TW_USER_", session),
		PasswordEnv: standalone.EnvName("TW_PASSWORD_", session), URLEnv: standalone.EnvName("TW_URL_", session), Login: d.WebLogin,
		Browser: d.Browser, Viewport: d.Viewport, Insecure: d.Insecure}
}

// specPath: where a test's spec goes (cases/x/y.yaml → tests/x/y.spec.ts) and its depth.
func specPath(rel string) (string, int) {
	r := strings.TrimPrefix(path.Clean(rel), "cases/")
	r = strings.TrimSuffix(r, path.Ext(r)) + ".spec.ts"
	return "tests/" + r, strings.Count("tests/"+r, "/")
}

// csvText writes rows as CSV.
func csvText(rows [][]string) string {
	var b strings.Builder
	for _, r := range rows {
		for i, c := range r {
			if i > 0 {
				b.WriteByte(',')
			}
			if strings.ContainsAny(c, ",\"\n\r") {
				c = `"` + strings.ReplaceAll(c, `"`, `""`) + `"`
			}
			b.WriteString(c)
		}
		b.WriteByte('\n')
	}
	return b.String()
}

const pwPackage = `{
  "name": "%s",
  "private": true,
  "scripts": { "test": "playwright test", "report": "playwright show-report" },
  "devDependencies": { "@playwright/test": "%s", "@types/node": "^22.0.0" }
}
`

const pwConfig = `import { defineConfig } from '@playwright/test';

// Written by Termwright. Change freely: https://playwright.dev/docs/test-configuration
export default defineConfig({
  testDir: './tests',
  timeout: 120_000,
  expect: { timeout: 10_000 },
  retries: 0,
  reporter: [['list'], ['html', { open: 'never' }], ['junit', { outputFile: 'reports/junit.xml' }]],
  use: { actionTimeout: 30_000, screenshot: 'only-on-failure', video: 'retain-on-failure', trace: 'retain-on-failure' },
});
`

const pwTSConfig = `{
  "compilerOptions": { "target": "ES2022", "module": "commonjs", "strict": true, "esModuleInterop": true, "resolveJsonModule": true, "types": ["node"] }
}
`

const pwHelper = `// Helpers of the tests Termwright wrote: targets as Termwright records them, the web apps' logins,
// {{variables}} and test data. Passwords come from the environment, never from a file.
import { test, expect, type BrowserContext, type Locator, type Page } from '@playwright/test';
import * as fs from 'fs';
import * as path from 'path';
import apps from './apps.json';

type Action = { do: string; target?: string; value?: string };
type App = { url: string; user?: string; userEnv: string; passwordEnv: string; urlEnv: string; login?: Action[] };
const APPS = apps as Record<string, App>;

// Pictures in the report, in the step they belong to: of a failed step (TW_FAIL_SHOTS=0: none) and after
// every step (TW_STEP_SHOTS=1; bigger reports).
const env = (k: string, d: boolean) => (process.env[k] ? !['0', 'false', 'no', 'off'].includes(process.env[k]!.toLowerCase()) : d);
const FAIL_SHOTS = env('TW_FAIL_SHOTS', __FAIL_SHOTS__);
const STEP_SHOTS = env('TW_STEP_SHOTS', __STEP_SHOTS__);

function app(name: string): App {
  const a = APPS[name];
  if (!a) throw new Error('web app ' + name + ' is not in apps.json');
  return a;
}

export const tw = {
  /** A Termwright target: label=…, placeholder=…, testid=…, alt=…, title=…, //xpath, frame=… >> …, or any Playwright selector. */
  loc(p: Page, t: string): Locator {
    let root: Page | ReturnType<Page['frameLocator']> = p;
    const parts = t.split(/\s*>>\s*/);
    while (parts.length > 1 && parts[0].startsWith('frame=')) root = root.frameLocator(parts.shift()!.slice(6));
    t = parts.join(' >> ');
    if (t.startsWith('label=')) return root.getByLabel(t.slice(6), { exact: false });
    if (t.startsWith('placeholder=')) return root.getByPlaceholder(t.slice(12));
    if (t.startsWith('testid=')) return root.getByTestId(t.slice(7));
    if (t.startsWith('alt=')) return root.getByAltText(t.slice(4));
    if (t.startsWith('title=')) return root.getByTitle(t.slice(6));
    if (t.startsWith('//')) return root.locator('xpath=' + t);
    return root.locator(t);
  },

  /** The target, or the first of its fallbacks that is on the page (the page changed since the recording). */
  async find(p: Page, t: string, alt: string[] = []): Promise<Locator> {
    const first = tw.loc(p, t).first();
    if (!alt.length) return first;
    try { await first.waitFor({ state: 'attached', timeout: 5000 }); return first; } catch { /* try the fallbacks */ }
    for (const a of alt) {
      const l = tw.loc(p, a).first();
      if (await l.count() > 0) { console.log('healed: ' + t + ' -> ' + a); return l; }
    }
    return first;
  },

  /** The test's values: its own, TW_VAR_<name> from the environment, then the data row. */
  vars(own: Record<string, string>, row: Record<string, string> = {}): Record<string, string> {
    const v: Record<string, string> = { ...own };
    for (const [k, x] of Object.entries(process.env)) if (k.startsWith('TW_VAR_') && x !== undefined) v[k.slice(7)] = x;
    return Object.assign(v, row);
  },

  /** s with its {{name}} filled. */
  v(s: string, v: Record<string, string>): string {
    return s.replace(/\{\{\s*([\w.-]+)\s*\}\}/g, (m, k) => {
      if (k === 'password') throw new Error('{{password}} comes from a web app login');
      if (!(k in v)) throw new Error(m + ' has no value: set it in the test, TW_VAR_' + k + ' or the data file');
      return v[k];
    });
  },

  /** A test data file (CSV with a header row) as rows. */
  rows(file: string): Record<string, string>[] {
    const text = fs.readFileSync(path.join(__dirname, file), 'utf8').replace(/^\uFEFF/, '');
    const recs: string[][] = []; let rec: string[] = [], cell = '', q = false;
    for (let i = 0; i < text.length; i++) {
      const c = text[i];
      if (q) { if (c === '"' && text[i + 1] === '"') { cell += '"'; i++; } else if (c === '"') q = false; else cell += c; }
      else if (c === '"') q = true;
      else if (c === ',') { rec.push(cell); cell = ''; }
      else if (c === '\n' || c === '\r') { if (c === '\r' && text[i + 1] === '\n') i++; rec.push(cell); cell = ''; if (rec.some(x => x !== '')) recs.push(rec); rec = []; }
      else cell += c;
    }
    rec.push(cell); if (rec.some(x => x !== '')) recs.push(rec);
    const [head, ...body] = recs;
    return body.map(r => Object.fromEntries(head.map((h, i) => [h.trim(), (r[i] ?? '').trim()])));
  },

  /** A web app's address joined with a path of the step. */
  url(name: string, rel: string, v: Record<string, string>): string {
    const base = (process.env[app(name).urlEnv] || app(name).url).replace(/\/+$/, '');
    rel = tw.v(rel || '', v);
    if (/^[a-z]+:\/\//i.test(rel)) return rel;
    return rel ? base + '/' + rel.replace(/^\/+/, '') : base;
  },

  user(name: string): string {
    return process.env[app(name).userEnv] || app(name).user || '';
  },

  password(name: string): string {
    const a = app(name), pw = process.env[a.passwordEnv] || process.env.TW_PASSWORD;
    if (!pw) throw new Error('set ' + a.passwordEnv + ' (or TW_PASSWORD) to the password of ' + name);
    return pw;
  },

  /** Logs in with the web app's recorded login when the page asks for it (its first field shows). */
  async login(p: Page, name: string, v: Record<string, string>): Promise<void> {
    const steps = app(name).login || [];
    const first = steps.find(s => s.target);
    if (!first) return;
    try { await tw.loc(p, first.target!).first().waitFor({ state: 'visible', timeout: 5000 }); } catch { return; } // logged in already
    for (const s of steps) {
      const val = (s.value || '').replace(/\{\{\s*username\s*\}\}/g, tw.user(name)).replace(/\{\{\s*password\s*\}\}/g, () => tw.password(name));
      const el = s.target ? tw.loc(p, s.target).first() : null;
      switch (s.do) {
        case 'goto': await p.goto(val); break;
        case 'fill': await el!.fill(val); break;
        case 'type': await el!.pressSequentially(val); break;
        case 'press': if (el) await el.press(val); else await p.keyboard.press(val); break;
        case 'select': await tw.select(el!, val); break;
        case 'check': await el!.check(); break;
        case 'wait': if (el) await el.waitFor(); else await p.waitForTimeout((parseFloat(val) || 1) * 1000); break;
        case 'expect': if (el) await expect(el).toBeVisible(); break;
        default: if (el) await el.click();
      }
    }
    await p.waitForLoadState();
  },

  async select(el: Locator, value: string): Promise<void> {
    try { await el.selectOption({ label: value }); } catch { await el.selectOption(value); }
  },

  /** The text of an element, or a field's value. */
  async read(el: Locator): Promise<string> {
    const field = await el.evaluate(n => ['INPUT', 'TEXTAREA', 'SELECT'].includes(n.tagName));
    return field ? el.inputValue() : (await el.innerText()).trim();
  },

  /** Files to upload, relative to the suite ("a.txt, b.txt"). */
  files(list: string): string[] {
    return list.split(',').map(f => f.trim()).filter(Boolean).map(f => path.resolve(__dirname, f));
  },

  async download(p: Page, el: Locator): Promise<void> {
    const [d] = await Promise.all([p.waitForEvent('download'), el.click()]);
    await d.saveAs(path.join(__dirname, 'downloads', d.suggestedFilename()));
  },

  /** Another tab or popup: 1, 2… or last, or a part of its address. */
  async tab(ctx: BrowserContext, p: Page, which: string): Promise<Page> {
    await p.waitForTimeout(300);
    const pages = ctx.pages();
    let next: Page | undefined;
    if (!which || which === 'last') next = pages[pages.length - 1];
    else if (/^\d+$/.test(which)) next = pages[Number(which) - 1];
    else next = pages.find(x => x.url().includes(which));
    if (!next) throw new Error('no tab ' + which);
    await next.bringToFront();
    return next;
  },

  /** A step of the test: on a failure (and after every step with STEP_SHOTS) the page is attached to it. */
  async step(name: string, page: () => Page, body: () => Promise<void>): Promise<void> {
    await test.step(name, async () => {
      const shot = async (what: string) => {
        try { await test.info().attach(name + ' - ' + what, { body: await page().screenshot({ fullPage: true }), contentType: 'image/png' }); } catch { /* no page */ }
      };
      try { await body(); } catch (e) { if (FAIL_SHOTS) await shot('failed'); throw e; }
      if (STEP_SHOTS) await shot('done');
    });
  },

  re(s: string): string {
    return s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
  },
};
`

const pwReadme = `# %s - Playwright tests

Written by Termwright from its tests: the web steps as Playwright Test (TypeScript). Steps that are not web
steps (device commands, API calls, manual steps) are comments in the specs - run those with Termwright or an
exported Python/Go suite.

## Run

    npm install
    npx playwright install chromium
%s    npx playwright test          # all tests; npx playwright test tests/x.spec.ts for one
    npx playwright show-report   # the HTML report (also reports/junit.xml)

## Logins and values

- The web apps, their addresses and recorded logins are in **apps.json** - never a password.
- Passwords come from the environment: the variables above (or TW_PASSWORD for all apps).
  A user name can be set the same way (TW_USER_<APP>), an address with TW_URL_<APP>.
- Values of the tests ({{name}}) can be changed with TW_VAR_<name>.
- Tests with test data run once per row of their file in **data/**.
- tw.ts has the helpers: targets as Termwright records them (label=…, testid=…, fallbacks), the login, the values.
%s`

// exportPlaywright packs the tests' web steps as a Playwright Test project (TypeScript).
func (f Folder) exportPlaywright(o ExportOptions, cases []string, name string, put func(string, []byte, bool) error) error {
	apps := map[string]*pwApp{}
	type spec struct {
		rel, text string
	}
	var specs []spec
	var skipped []string
	for _, rel := range cases {
		c, err := f.Load(rel)
		if err != nil {
			return fmt.Errorf("%s: %v", rel, err)
		}
		if p, err := f.Prepare(c); err == nil {
			c = p
		}
		for _, list := range [][]Step{c.Setup, c.Steps} {
			for _, st := range list {
				if st.Web != nil && st.Web.Login != "" && apps[st.Web.Login] == nil && o.Device != nil {
					if d, ok := o.Device(st.Web.Login); ok {
						apps[st.Web.Login] = playwrightApp(st.Web.Login, d)
					}
				}
				if st.Web != nil && st.Web.Login != "" && apps[st.Web.Login] == nil { // not a saved web app: its address only
					apps[st.Web.Login] = &pwApp{UserEnv: standalone.EnvName("TW_USER_", st.Web.Login), PasswordEnv: standalone.EnvName("TW_PASSWORD_", st.Web.Login), URLEnv: standalone.EnvName("TW_URL_", st.Web.Login)}
				}
			}
		}
		data := ""
		if c.Data != "" {
			b, err := os.ReadFile(filepath.Join(f.Root, c.Data))
			if err != nil {
				return fmt.Errorf("%s: its test data %s: %v", rel, c.Data, err)
			}
			if strings.EqualFold(filepath.Ext(c.Data), ".xlsx") {
				rows, err := XLSXRows(b)
				if err != nil {
					return fmt.Errorf("%s: %v", c.Data, err)
				}
				b = []byte(csvText(rows))
			}
			base := filepath.Base(c.Data)
			data = "data/" + strings.TrimSuffix(base, filepath.Ext(base)) + ".csv"
			if err := put(data, b, false); err != nil {
				return err
			}
		}
		sp, depth := specPath(rel)
		text, ok := PlaywrightSpec(c, depth, data, apps)
		if !ok {
			skipped = append(skipped, rel)
			continue
		}
		specs = append(specs, spec{sp, text})
	}
	if len(specs) == 0 {
		return fmt.Errorf("none of these tests has a web step: a Playwright export has the web steps")
	}
	for _, s := range specs {
		if err := put(s.rel, []byte(s.text), false); err != nil {
			return err
		}
	}
	aj, _ := json.MarshalIndent(apps, "", "  ")
	var env strings.Builder
	keys := make([]string, 0, len(apps))
	for k := range apps {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if len(apps[k].Login) > 0 {
			if o.OS == "windows" {
				fmt.Fprintf(&env, "    $env:%s = '…'   # %s\n", apps[k].PasswordEnv, k)
			} else {
				fmt.Fprintf(&env, "    export %s='…'   # %s\n", apps[k].PasswordEnv, k)
			}
		}
	}
	note := ""
	if len(skipped) > 0 {
		note = "\nNot here (no web steps): " + strings.Join(skipped, ", ") + ".\n"
	}
	files := map[string]string{
		"package.json":         fmt.Sprintf(pwPackage, name, PlaywrightVersion),
		"playwright.config.ts": pwConfig,
		"tsconfig.json":        pwTSConfig,
		"tw.ts":                strings.NewReplacer("__FAIL_SHOTS__", strconv.FormatBool(!o.NoFailShots), "__STEP_SHOTS__", strconv.FormatBool(o.StepShots)).Replace(pwHelper),
		"apps.json":            string(aj) + "\n",
		".gitignore":           "node_modules/\ntest-results/\nplaywright-report/\nreports/\ndownloads/\n",
		"README.md":            fmt.Sprintf(pwReadme, name, env.String(), note),
	}
	for rel, text := range files {
		if err := put(rel, []byte(text), false); err != nil {
			return err
		}
	}
	return nil
}

func orStr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
