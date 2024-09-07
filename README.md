# 🐦 Budge

An opinionated tool for helping Kiwis with money and asset management.

Budge is powered by [Go](https://go.dev/), [Echo](https://echo.labstack.com/), 
[HTMX](https://htmx.org/), [Bootstrap](https://getbootstrap.com/), and [bbolt](https://github.com/etcd-io/bbolt),

![Budge](./web/public/budge_circle_300.png)

## Integrations

Budge integrates with [Akahu](https://www.akahu.nz/) in order to allow you to see your accounts and transactions.  

## Features

To see what is in the pipeline, have a look at the [roadmap](./docs/roadmap)

## Getting Started

Budge has been built with self-hosting in mind. Other tools that do similiar things are available, 
but this one is completely free!

To leverage the most out of Budge, we recommend creating an account with Akahu, setting up a personal app, and using that
to retrieve your data.

This will involve creating a profile, verifying your identity, and setting up MFA. More information can be found [here](https://developers.akahu.nz/docs/personal-apps)

Once you have a user and app token, download the latest release, and open `http://localhost:1337` in your browser.

Navigate to settings, load in your credentials, and perform your first sync!

## Contributing

If you'd like to contribute, please read [CONTRIBUTING.MD](./docs/CONTRIBUTING.md) to get started!