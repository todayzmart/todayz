# apigen fixtures

Each directory is one contract that `TestFixtures` (main_test.go) joins with the
real `api/root.yaml`, `common/`, `problems/` and `schemas/common.yaml`, then runs
through `apigen root` and `apigen check`. `valid-orders` passes and is the
acceptance example (`TestCodegenRoundTrip` generates its Go package); every other
directory breaks one rule and must fail with the message the test names. A
fixture without a `schemas/` directory borrows `valid-orders/schemas/`.
