package analyzer

import (
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestParseSource_Metrics(t *testing.T) {
	tests := []struct {
		name       string
		lang       string
		path       string
		src        string
		cyclomatic int
		cognitive  int
		functions  int
		exported   int
		imports    []string
		packages   []string
	}{
		{
			name: "rust",
			lang: "Rust",
			path: "src/server.rs",
			src: `mod net;
pub mod util;
use crate::config::{Config, load as load_cfg};
use super::parent::Thing;
use std::collections::HashMap;

pub struct Server;

pub fn run(v: i32) -> i32 {
    if v > 0 && v < 10 {
        return 1;
    } else if v == 0 {
        return 0;
    }
    for i in 0..v {
        while i > 2 {
            break;
        }
    }
    match v {
        1 => 1,
        _ => 2,
    }
}

fn helper(xs: &[i32]) -> usize {
    xs.iter().filter(|x| **x > 0 || **x < -5).count()
}
`,
			// 2 functions + if, &&, else-if, for, while, 2 arms, ||
			cyclomatic: 10,
			// if 1, else-if 1, for 1, while 1+1, match 1
			cognitive: 6,
			functions: 2,
			exported:  3,
			imports: []string{
				"crate::config::Config", "crate::config::load", "mod:net", "mod:util",
				"std::collections::HashMap", "super::parent::Thing",
			},
		},
		{
			name: "typescript",
			lang: "TypeScript",
			path: "src/run.ts",
			src: `import { a } from "./util";
import b from "../lib/b";
import * as c from "@app/core";
export { d } from "./d";
const e = require("./e");

export function run(v: number): number {
  if (v > 0 && v < 10) {
    return 1;
  } else if (v === 0) {
    return 0;
  }
  for (const x of [1, 2]) {
    while (x > v) {
      v++;
    }
  }
  switch (v) {
    case 1:
      return 1;
    default:
      return 2;
  }
}

export const twice = (n: number) => (n > 0 ? n * 2 : 0);

export class Box {
  open(): boolean {
    try {
      return true;
    } catch (err) {
      return false;
    }
  }
}
`,
			// 3 functions + if, &&, else-if, for, while, 2 cases, ternary, catch
			cyclomatic: 12,
			// run: if, else-if, for, while(2), switch = 6; twice: ternary 1; open: catch 1
			cognitive: 8,
			functions: 3,
			exported:  4,
			imports:   []string{"../lib/b", "./d", "./e", "./util", "@app/core"},
		},
		{
			name: "javascript commonjs and esm",
			lang: "JavaScript",
			path: "src/go.js",
			src: `const { x } = require("./x");
import y from "./y.js";
function go(a) { return a || x(y); }
module.exports = { go };
`,
			cyclomatic: 2,
			cognitive:  0,
			functions:  1,
			exported:   0,
			imports:    []string{"./x", "./y.js"},
		},
		{
			name: "javascript esm module (.mjs)",
			lang: "JavaScript",
			path: "src/server.mjs",
			src: `import { readFile } from "node:fs/promises";
import { route } from "./routes.mjs";
export * from "./types.js";

export async function handle(req) {
  for (const r of route(req)) {
    if (r.ok && r.body) {
      return r;
    }
  }
  const mod = await import("./lazy.mjs");
  return mod.fallback ?? null;
}
`,
			// 1 function + for, if, &&
			cyclomatic: 4,
			// for 1, if 1+1
			cognitive: 3,
			functions: 1,
			exported:  1,
			imports:   []string{"./lazy.mjs", "./routes.mjs", "./types.js", "node:fs/promises"},
		},
		{
			name: "java",
			lang: "Java",
			path: "src/main/java/com/acme/app/Service.java",
			src: `package com.acme.app;

import com.acme.util.Helper;
import com.acme.model.*;
import static com.acme.util.Strings.trim;
import java.util.List;

public class Service {
    private int count;
    public String name;

    public Service() {}

    public int run(int v) {
        if (v > 0 || v < -10) {
            return 1;
        } else if (v == 0) {
            return 0;
        }
        for (int i = 0; i < v; i++) {
            if (i % 2 == 0) {
                count++;
            }
        }
        try {
            Helper.call();
        } catch (RuntimeException e) {
            return -1;
        }
        return v > 1 ? 1 : 2;
    }

    private void reset() {
        count = 0;
    }
}
`,
			cyclomatic: 10,
			cognitive:  7,
			functions:  3,
			exported:   4,
			imports:    []string{"com.acme.model.*", "com.acme.util.Helper", "com.acme.util.Strings.trim", "java.util.List"},
			packages:   []string{"com.acme.app"},
		},
		{
			name: "csharp",
			lang: "C#",
			path: "src/Worker.cs",
			src: `using System;
using Acme.Util;
namespace Acme.App;

public class Worker
{
    public string Name { get; set; }

    public int Run(int v)
    {
        if (v > 0 && v < 10) { return 1; }
        foreach (var x in Items()) { if (x == v) { return x; } }
        switch (v) { case 1: return 1; default: return 2; }
    }

    private int[] Items() => new[] { 1, 2 };
}
`,
			cyclomatic: 8,
			cognitive:  5,
			functions:  2,
			exported:   3,
			imports:    []string{"Acme.Util", "System"},
			packages:   []string{"Acme.App"},
		},
		{
			name: "c",
			lang: "C",
			path: "src/run.c",
			src: `#include "util.h"
#include <stdio.h>

static int helper(int x) { return x > 0 ? x : -x; }

int run(int v) {
    if (v > 0 && v < 10) {
        return 1;
    }
    while (v > 100) {
        v /= 2;
    }
    switch (v) {
    case 1:
        return 1;
    default:
        return helper(v);
    }
}
`,
			cyclomatic: 8,
			cognitive:  4,
			functions:  2,
			exported:   1,
			imports:    []string{"\"util.h\"", "<stdio.h>"},
		},
		{
			name: "cpp",
			lang: "C++",
			path: "src/run.cpp",
			src: `#include "lib/util.hpp"
namespace acme {
int run(int v) {
    try {
        for (auto x : {1, 2}) {
            if (x == v || x > 9) return x;
        }
    } catch (...) {
        return -1;
    }
    auto f = [](int x) { return x > 0 ? 1 : 0; };
    return f(v);
}
}
`,
			// 1 function + for, if, ||, catch, ternary
			cyclomatic: 6,
			// for 1, if 2, catch 1, ternary inside lambda 1+1
			cognitive: 6,
			functions: 1,
			exported:  1,
			imports:   []string{"\"lib/util.hpp\""},
		},
		{
			name: "ruby",
			lang: "Ruby",
			path: "lib/acme/runner.rb",
			src: `require_relative "support/util"
require "acme/model"

module Acme
  class Runner
    def run(v)
      if v > 0 && v < 10
        1
      elsif v == 0
        0
      end
      items.each do |x|
        return x if x == v
      end
      case v
      when 1 then :one
      when 2 then :two
      end
    end

    def self.build
      new
    end

    private

    def secret
      raise "no" unless ok?
    rescue StandardError
      nil
    end
  end
end
`,
			// 3 functions + if, &&, elsif, if-modifier, 2 whens, unless-modifier, rescue
			cyclomatic: 11,
			// run: if 1, elsif 1, if-modifier in block 2, case 1; secret: unless 1, rescue 1
			cognitive: 7,
			functions: 3,
			exported:  4,
			imports:   []string{"require:acme/model", "require_relative:support/util"},
		},
		{
			name: "kotlin",
			lang: "Kotlin",
			path: "src/main/kotlin/com/acme/app/Runner.kt",
			src: `package com.acme.app

import com.acme.util.Helper
import com.acme.model.*

class Runner {
    fun run(v: Int): Int {
        if (v > 0 && v < 10) {
            return 1
        } else if (v == 0) {
            return 0
        }
        for (i in 0 until v) {
            while (i > 3) {
                break
            }
        }
        return when (v) {
            1 -> 1
            else -> 2
        }
    }

    private fun hidden() = Helper.call()
}

internal fun internalOnly() {}
`,
			cyclomatic: 10,
			cognitive:  6,
			functions:  3,
			exported:   2,
			imports:    []string{"com.acme.model.*", "com.acme.util.Helper"},
			packages:   []string{"com.acme.app"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := parseSource(tt.lang, tt.path, []byte(tt.src))
			if err != nil {
				t.Fatalf("parseSource: %v", err)
			}
			if res.Cyclomatic != tt.cyclomatic {
				t.Errorf("Cyclomatic = %d, want %d", res.Cyclomatic, tt.cyclomatic)
			}
			if res.Cognitive != tt.cognitive {
				t.Errorf("Cognitive = %d, want %d", res.Cognitive, tt.cognitive)
			}
			if res.Functions != tt.functions {
				t.Errorf("Functions = %d, want %d", res.Functions, tt.functions)
			}
			if res.Exported != tt.exported {
				t.Errorf("Exported = %d, want %d", res.Exported, tt.exported)
			}
			got := append([]string{}, res.Imports...)
			sort.Strings(got)
			if !reflect.DeepEqual(got, tt.imports) {
				t.Errorf("Imports = %q, want %q", got, tt.imports)
			}
			if !reflect.DeepEqual(res.Packages, tt.packages) {
				t.Errorf("Packages = %q, want %q", res.Packages, tt.packages)
			}
		})
	}
}

func TestParseSource_DeclaredSymbols(t *testing.T) {
	tests := []struct {
		name, lang, path, src string
		want                  []string
	}{
		{
			name: "kotlin top-level declarations carry the package",
			lang: "Kotlin", path: "Services.kt",
			src:  "package com.example\n\nclass PaymentService { fun member() {} }\ninterface Ledger\nobject Registry\nfun formatAmount() { val local = 1 }\nval ZERO = 0L\n",
			want: []string{"com.example.Ledger", "com.example.PaymentService", "com.example.Registry", "com.example.ZERO", "com.example.formatAmount"},
		},
		{
			name: "kotlin without a package uses the bare name",
			lang: "Kotlin", path: "Main.kt",
			src:  "fun main() {}\n",
			want: []string{"main"},
		},
		{
			name: "csharp block and nested namespaces skip nested types",
			lang: "C#", path: "Handlers.cs",
			src:  "namespace Acme { namespace Billing { public class Processor { public class Nested {} } public enum Kind {} } }\nclass Global {}\n",
			want: []string{"Acme.Billing.Kind", "Acme.Billing.Processor", "Global"},
		},
		{
			name: "csharp file-scoped namespace",
			lang: "C#", path: "Text.cs",
			src:  "namespace Acme.Util;\n\npublic static class StringTools {}\npublic record Money();\n",
			want: []string{"Acme.Util.Money", "Acme.Util.StringTools"},
		},
		{
			name: "java keeps the filename rule and declares no symbols",
			lang: "Java", path: "Helper.java",
			src:  "package com.acme;\npublic class Helper {}\n",
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := parseSource(tt.lang, tt.path, []byte(tt.src))
			if err != nil {
				t.Fatalf("parseSource: %v", err)
			}
			var got []string
			if res.Symbols != nil {
				got = append(got, res.Symbols...)
				sort.Strings(got)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Symbols = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseSource_Errors(t *testing.T) {
	tests := []struct {
		name string
		lang string
		src  string
	}{
		{"unsupported language", "Python", "x = 1\n"},
		{"oversized file falls back", "Rust", "fn a() {}\n" + strings.Repeat("// pad\n", maxParseBytes/7+1)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := parseSource(tt.lang, "x", []byte(tt.src)); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestParseSource_EmptyFile(t *testing.T) {
	res, err := parseSource("TypeScript", "empty.ts", nil)
	if err != nil {
		t.Fatalf("parseSource: %v", err)
	}
	if res.Cyclomatic != 0 || res.Cognitive != 0 || res.Functions != 0 || len(res.Imports) != 0 {
		t.Errorf("empty file metrics = %+v, want zeros", res)
	}
}

// analyzeRepo writes files to a temp repo, runs Analyze and returns the analyzer.
func analyzeRepo(t *testing.T, files map[string]string) *Analyzer {
	t.Helper()
	a := New(writeRepo(t, files), nil, nil)
	if err := a.Analyze(); err != nil {
		t.Fatal(err)
	}
	return a
}

func assertEdges(t *testing.T, a *Analyzer, wantImportedBy map[string][]string, wantImports map[string][]string) {
	t.Helper()
	for path, want := range wantImportedBy {
		info, ok := a.GetFileInfo(path)
		if !ok {
			t.Fatalf("missing file %s", path)
		}
		got := append([]string{}, info.ImportedBy...)
		sort.Strings(got)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s ImportedBy = %v, want %v", path, got, want)
		}
	}
	for path, want := range wantImports {
		info, ok := a.GetFileInfo(path)
		if !ok {
			t.Fatalf("missing file %s", path)
		}
		got := append([]string{}, info.Imports...)
		sort.Strings(got)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s Imports = %v, want %v", path, got, want)
		}
	}
}

func TestAnalyzer_RustImports(t *testing.T) {
	a := analyzeRepo(t, map[string]string{
		"Cargo.toml":     "[package]\nname = \"my-app\"\nversion = \"0.1.0\"\n",
		"src/lib.rs":     "pub mod config;\nmod net;\nuse crate::config::Config;\n",
		"src/config.rs":  "pub struct Config;\n",
		"src/net/mod.rs": "mod tcp;\nuse super::config;\nuse self::tcp::Conn;\n",
		"src/net/tcp.rs": "pub struct Conn;\nuse crate::config::Config;\nuse std::io;\nuse serde::Serialize;\n",
		"tests/it.rs":    "use my_app::config::Config;\nuse other_crate::Thing;\n",
	})
	assertEdges(t, a,
		map[string][]string{
			"src/config.rs":  {"src/lib.rs", "src/net/mod.rs", "src/net/tcp.rs", "tests/it.rs"},
			"src/net/mod.rs": {"src/lib.rs"},
			"src/net/tcp.rs": {"src/net/mod.rs"},
			"src/lib.rs":     {},
			"tests/it.rs":    {},
		},
		map[string][]string{
			"src/net/tcp.rs": {"src/config.rs"},
			"tests/it.rs":    {"src/config.rs"},
		},
	)
	info, _ := a.GetFileInfo("src/lib.rs")
	if info.ExportedSymbols != 1 {
		t.Errorf("src/lib.rs ExportedSymbols = %d, want 1", info.ExportedSymbols)
	}
}

func TestAnalyzer_TypeScriptImports(t *testing.T) {
	a := analyzeRepo(t, map[string]string{
		"tsconfig.json": `{
  // comments and trailing commas are allowed in tsconfig
  "compilerOptions": {
    "baseUrl": ".",
    "paths": { "@core/*": ["src/core/*"] },
  },
}`,
		"src/core/math.ts":   "export const add = (a: number, b: number) => a + b;\n",
		"src/core/index.ts":  "export * from './math';\n",
		"src/util/format.js": "export function fmt(x) { return String(x); }\n",
		"src/lib/helper.ts":  "export const h = 1;\n",
		"src/app.tsx": `import { add } from "@core/math";
import * as core from "./core";
import { fmt } from "./util/format.js";
import { h } from "./lib/helper.js";
import React from "react";
import { missing } from "./does-not-exist";
export const App = () => <div>{add(1, 2)}</div>;
`,
		"src/legacy.js": "const f = require(\"./util/format\");\n",
		// ESM: explicit .mjs/.cjs extensions, and TS's "./x.js" -> x.ts convention
		"src/esm/main.mjs":  "import { dep } from \"./dep.mjs\";\nimport cfg from \"./cfg.cjs\";\nimport { h } from \"../lib/helper.js\";\nimport { t } from \"./typed.mjs\";\n",
		"src/esm/dep.mjs":   "export const dep = 1;\n",
		"src/esm/cfg.cjs":   "module.exports = {};\n",
		"src/esm/typed.mts": "export const t = 1;\n",
	})
	assertEdges(t, a,
		map[string][]string{
			"src/core/math.ts":   {"src/app.tsx", "src/core/index.ts"},
			"src/core/index.ts":  {"src/app.tsx"},
			"src/util/format.js": {"src/app.tsx", "src/legacy.js"},
			"src/lib/helper.ts":  {"src/app.tsx", "src/esm/main.mjs"},
			"src/esm/dep.mjs":    {"src/esm/main.mjs"},
			"src/esm/cfg.cjs":    {"src/esm/main.mjs"},
			"src/esm/typed.mts":  {"src/esm/main.mjs"},
			"src/app.tsx":        {},
		},
		map[string][]string{
			"src/app.tsx":      {"src/core/index.ts", "src/core/math.ts", "src/lib/helper.ts", "src/util/format.js"},
			"src/esm/main.mjs": {"src/esm/cfg.cjs", "src/esm/dep.mjs", "src/esm/typed.mts", "src/lib/helper.ts"},
		},
	)
}

func TestAnalyzer_JavaImports(t *testing.T) {
	base := "src/main/java/com/acme/"
	a := analyzeRepo(t, map[string]string{
		base + "util/Helper.java": "package com.acme.util;\npublic class Helper { public static void call() {} }\n",
		base + "model/User.java":  "package com.acme.model;\npublic class User {}\n",
		base + "model/Order.java": "package com.acme.model;\npublic class Order {}\n",
		base + "app/Service.java": "package com.acme.app;\nimport com.acme.util.Helper;\nimport com.acme.model.*;\nimport static com.acme.util.Helper.call;\nimport java.util.List;\npublic class Service {}\n",
	})
	assertEdges(t, a,
		map[string][]string{
			base + "util/Helper.java": {base + "app/Service.java"},
			base + "model/User.java":  {base + "app/Service.java"},
			base + "model/Order.java": {base + "app/Service.java"},
			base + "app/Service.java": {},
		},
		map[string][]string{
			base + "app/Service.java": {base + "model/Order.java", base + "model/User.java", base + "util/Helper.java"},
		},
	)
}

func TestAnalyzer_KotlinImportsResolveDeclaredSymbols(t *testing.T) {
	base := "src/main/kotlin/com/example/"
	a := analyzeRepo(t, map[string]string{
		base + "Services.kt":                    "package com.example\n\nclass PaymentService\ninterface Ledger\nobject Registry\n",
		base + "util/Money.kt":                  "package com.example.util\n\nfun formatAmount(c: Long): String = c.toString()\nval ZERO = 0L\n",
		base + "util/Other.kt":                  "package com.example.util\n\nclass Unrelated\n",
		base + "app/Checkout.kt":                "package com.example.app\n\nimport com.example.PaymentService\nimport com.example.util.formatAmount\nimport kotlin.math.max\n\nclass Checkout\n",
		base + "app/Audit.kt":                   "package com.example.app\n\nimport com.example.util.ZERO\nimport com.example.Registry\n",
		"src/main/java/com/example/Caller.java": "package com.example.j;\nimport com.example.Ledger;\npublic class Caller {}\n",
	})
	assertEdges(t, a, nil, map[string][]string{
		base + "app/Checkout.kt":                {base + "Services.kt", base + "util/Money.kt"},
		base + "app/Audit.kt":                   {base + "Services.kt", base + "util/Money.kt"},
		"src/main/java/com/example/Caller.java": {base + "Services.kt"},
	})
}

func TestAnalyzer_CSharpUsingsResolveDeclaredTypes(t *testing.T) {
	a := analyzeRepo(t, map[string]string{
		"src/Billing/Handlers.cs": "namespace Acme.Billing\n{\n    public class PaymentProcessor {}\n    public class Outer { public class Nested {} }\n}\n",
		"src/Util/Text.cs":        "namespace Acme.Util;\n\npublic static class StringTools {}\n",
		"src/Util/Unrelated.cs":   "namespace Acme.Util;\n\npublic class Other {}\n",
		"src/App/Program.cs":      "using static Acme.Util.StringTools;\nusing Proc = Acme.Billing.PaymentProcessor;\nusing System;\n\nnamespace Acme.App;\npublic class Program {}\n",
	})
	assertEdges(t, a, nil, map[string][]string{
		"src/App/Program.cs": {"src/Billing/Handlers.cs", "src/Util/Text.cs"},
	})
}

func TestAnalyzer_TreeSitterComplexityReplacesHeuristic(t *testing.T) {
	a := analyzeRepo(t, map[string]string{
		"src/a.rs": "pub fn f(v: i32) -> i32 {\n    if v > 0 {\n        if v > 1 { return 2; }\n    }\n    0\n}\n",
	})
	info, _ := a.GetFileInfo("src/a.rs")
	// 1 function + 2 ifs; nested if costs 1+1
	if info.Cyclomatic != 3 || info.Cognitive != 3 || info.FunctionCount != 1 || info.ExportedSymbols != 1 {
		t.Errorf("got cyclomatic=%d cognitive=%d functions=%d exported=%d, want 3 3 1 1",
			info.Cyclomatic, info.Cognitive, info.FunctionCount, info.ExportedSymbols)
	}
}

func TestParseSource_ElseIfAndClosuresConsistentAcrossLanguages(t *testing.T) {
	// if/else-if costs 2; a closure bound to a variable is a function, so an
	// if inside it costs 1. JS used to differ because Node.Parent() returns a
	// hidden supertype node in that grammar.
	tests := []struct {
		lang, path, src string
		want            int
	}{
		{"JavaScript", "f.js", "function F() {\n  if (a) {\n  } else if (b) {\n  }\n}\n", 2},
		{"JavaScript", "f.mjs", "function F() {\n  if (a) {\n  } else if (b) {\n  }\n}\n", 2},
		{"TypeScript", "f.ts", "function F() {\n  if (a) {\n  } else if (b) {\n  }\n}\n", 2},
		{"Java", "F.java", "class F { void f() { if (a) { } else if (b) { } } }", 2},
		{"Rust", "f.rs", "fn f() { if a { } else if b { } }", 2},
		{"JavaScript", "g.js", "const g = () => {\n  if (a) {}\n};\n", 1},
		{"TypeScript", "g.ts", "const g = () => {\n  if (a) {}\n};\n", 1},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			r, err := parseSource(tt.lang, tt.path, []byte(tt.src))
			if err != nil {
				t.Fatal(err)
			}
			if r.Cognitive != tt.want {
				t.Errorf("cognitive = %d, want %d", r.Cognitive, tt.want)
			}
		})
	}
}
