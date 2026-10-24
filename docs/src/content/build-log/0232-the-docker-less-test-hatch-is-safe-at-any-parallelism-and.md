---
title: "The Docker-less test hatch is safe at any parallelism, and the gate's own default is 1"
date: 2026-08-13
refs: []
form: bullet
---

- **The Docker-less test hatch is safe at any parallelism, and the gate's own default is 1**
  ([#662](https://github.com/hyperscaleav/omniglass/issues/662)). `OMNIGLASS_TEST_ADMIN_DSN` points
  the harness at an already-running Postgres instead of starting a testcontainer, for environments
  with no Docker daemon. Two names made it unusable with more than one test binary at a time:
  `og_test_<n>`, from a counter that starts at 1 in every process, and `og_template`, a fixed name
  that the template build drops and recreates, so one binary could drop the template another was
  copying from mid-run. Both now carry a per-process tag (the pid, which separates binaries on one
  host, plus a random half, which separates a shared Postgres reached from two machines and a pid
  reused after a crash), and the template teardown takes a name rather than reading the package's, so
  "the template this binary built" is a property of the call.

  Exposure was zero: nothing sets the variable, and the hatch was already unusable concurrently
  because of the first collision. It is fixed so that raising parallelism on a bigger machine does not
  spring a trap that was set here.

  The gate's own `TEST_PARALLEL` default drops from 4 to 1 in the same change, on the architect's
  ruling. Four concurrent testcontainer Postgres instances plus four link jobs exhausted a WSL2 VM's
  memory and took the whole VM down, not just the run, and a gate that can take the machine with it is
  not a gate. It is a floor rather than a ceiling: the override the comment already documented
  (`make test TEST_PARALLEL=8`) is unchanged, and a machine with the memory for it should raise it.

  One expectation moved, deliberately, and it is the point of the change rather than a casualty of it:
  `provision_test.go` asserted the template's whole literal name, `og_template`. It now asserts a
  prefix carrying this process's pid, and that exactly one database matches it. A test that pins the
  fixed name is a test that pins the defect.
