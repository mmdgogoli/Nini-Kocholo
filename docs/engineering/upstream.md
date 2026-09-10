# Upstream relationship

Heimdall is derived from the open-source
[3X-UI project](https://github.com/MHSanaei/3x-ui).

The inherited Go module remains:

```text
github.com/mhsanaei/3x-ui/v3
```

Keeping this technical identity avoids a high-risk import-path migration and
reduces conflicts while integrating upstream changes. Heimdall releases,
documentation, issue tracking, installers, and container images remain owned
by the Heimdall repository.

Upstream authors retain attribution under GPL-3.0 and Git history.
