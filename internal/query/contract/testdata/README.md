# Synthetic T01 contract fixtures

These seven JSON fixtures are written from the field tables in
`sicp-api-design.md` sections 4–7. They contain no real person or upstream data.
The expected field sets are independent of Go struct reflection and generation.

Request examples deliberately include null and empty arrays so DTO tests can
distinguish them. These are transport fidelity cases, not validation acceptance
cases. P06 will reject empty filters as specified by the business contract.

The shared grades response covers both operations 4 and 5. Timetable fixtures
cover absent versus zero enrollment/credits, empty arrays, leading-zero IDs,
different teacher/name array lengths and zoned timestamps.
