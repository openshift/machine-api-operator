# Machine API Docusaurus

This directory contains the configuration and artifacts for building a
[Docusaurus](https://docusaurus.io/) based static web site for the Machine
API documentation.

## Adding a new document to the site

If you wish to add a new Markdown file to the documentation site, there are a
things you will need to do.

1. Add the appropriate [header material](https://docusaurus.io/docs/en/doc-markdown#markdown-headers)
   to your file. At the bare minimum you will need an `id` and a `title`.
2. Add your document to the `docusaurus/website/sidebars.js` file if you would
   like it to appear in the sidebar. Please note, you must use the `id` for
   your document and not the filename.

## Changing the site rendering or configuration

The main files for adjusting the site creation are `docusaurus/website/siteConfig.js`
and `docusaurus/website/pages/en/index.js`. You can read more about these files
in the [Docusaurus documentation](https://docusaurus.io/docs/en/installation).

## Building a local version of the site

The easiest way to build the site locally is using a container runtime like
[Podman](https://podman.io). Use the command `make docusaurus-image` to build
a local container image named `machine-api-operator-book:latest`.

If you wish to customize the build, please see the `docusaurus/Dockerfile` file
for more information.
