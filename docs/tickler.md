# Tickler

> The thing that tickles the pickle.

## What Is Tickler?

Tickler is Pickle's preprocessor. It takes the idiomatic Go you write — controllers, migrations, requests, routes — and transforms it into code that compiles alongside Pickle's generated output.

The problem: you write `Query[User]()` in a controller, but `Query[T]` doesn't exist until Pickle generates it. Your linter screams. Your IDE is useless. You can't run `go vet` on code that references types that don't exist yet.

Tickler bridges that gap. It processes your source files, resolves references to generated types, adds the correct imports, and produces compilable Go. You develop against real, lintable code. Tickler makes it build.

## The Pipeline

```
You write idiomatic Go
        ↓
   tickler runs
        ↓
Pickle-compatible source (compiles with generated output)
        ↓
   pickle generates
        ↓
Framework code (models, queries, routes, bindings, pickle.go)
        ↓
   go build
        ↓
Static binary
```

## Why?

Without Tickler, you have two bad options:

1. **Write against generated types** — your code doesn't lint or compile until after `pickle generate` runs. No IDE support, no `go vet`, no feedback loop while writing.
2. **Maintain stub types** — manually keep dummy type definitions in sync with what Pickle generates. Tedious, error-prone, defeats the purpose.

Tickler gives you a third option: write normal Go that references `Query[T]`, `Context`, `Response`, etc. as if they exist. Tickler knows what Pickle will generate and transforms your code to work with it.

## What Tickler Does

- Resolves references to generated types (`Query[T]`, `Context`, `Response`, `Router`, middleware types)
- Adds correct import paths pointing to the `generated/` package
- Transforms controller method signatures to match the generated binding interface
- Validates that migration DSL calls use known methods and column types
- Ensures `routes.go` references valid controller methods

## What Tickler Does NOT Do

- Generate models, queries, or route wiring — that's Pickle's job
- Modify your source files in place — Tickler outputs to a staging directory
- Run at runtime — Tickler is a build step, like Pickle itself

## Usage

```bash
# Tickle only (rarely needed standalone)
pickle tickle

# Generate always tickles first — tickle is a prerequisite, not optional
pickle generate   # tickle → generate

# Watch mode runs the full pipeline
pickle --watch    # tickle → generate → go build → restart
```

Tickle always runs before generation. There is no way to generate without tickling first — the generator expects tickled source as input. This is a linear pipeline, not a recursive compiler.

## Flow in Watch Mode

```
File saved
  → Tickler processes changed file
    → Pickle regenerates affected output
      → go build
        → Binary restarted
```

One save, full pipeline, no manual steps.
