# 🐦 Budge

An opinionated tool for helping Kiwis with money and asset management. 

Budge is powered by [Go](https://go.dev/), [Echo](https://echo.labstack.com/), 
[HTMX](https://htmx.org/), [Bootstrap](https://getbootstrap.com/), [Air](https://github.com/air-verse/air) and [bbolt](https://github.com/etcd-io/bbolt).

![Budge](./web/public/budge_circle_300.png)

## Getting Started

We integrate with [Akahu](https://www.akahu.nz/) in order to allow you to see your accounts and transactions. You will need to 
create a profile, verify your identity, and set up MFA in order to create a personal app for use with Budge. 

More information can be found [here](https://developers.akahu.nz/docs/personal-apps)

Once you have a user and app token, navigate to settings, load in your credentials, and perform your first sync!

## Deployment

TBD

## Development

To develop Budge you will need an Akahu account, as well as Golang installed:
- Akahu: https://developers.akahu.nz/docs/personal-apps
- Golang: https://go.dev/doc/install

Air is recommended in order to perform live-reloading of the build while changing files.

```shell
git clone https://github.com/TheQueenIsDead/budge.git
cd budge
go install github.com/air-verse/air@latest
air
# Budge is now accessible at http://localhost:1337
```
