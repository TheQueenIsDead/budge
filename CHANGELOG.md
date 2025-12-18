# Changelog

## [1.12.0](https://github.com/TheQueenIsDead/budge/compare/v1.11.0...v1.12.0) (2025-12-18)


### Features

* add in top transactions over the reporting period ([#55](https://github.com/TheQueenIsDead/budge/issues/55)) ([7f5fb40](https://github.com/TheQueenIsDead/budge/commit/7f5fb40c97da781f3cf7684da27bb2d30986ca36))
* consolidate settings page and fix table jumping out of div ([933caf5](https://github.com/TheQueenIsDead/budge/commit/933caf52119aefbdbb9c5bcd2073bac253a44b6c))
* tidy up the accounts page allignment and make buttons more consistent with theming ([a41cd78](https://github.com/TheQueenIsDead/budge/commit/a41cd7856dd42dd569e70013345eac5ba1a59610))

## [1.11.0](https://github.com/TheQueenIsDead/budge/compare/v1.10.0...v1.11.0) (2025-12-18)


### Features

* better account overview page ([#52](https://github.com/TheQueenIsDead/budge/issues/52)) ([800e5f7](https://github.com/TheQueenIsDead/budge/commit/800e5f75c59e9b5e21102d74e720522625a0d8b3))

## [1.10.0](https://github.com/TheQueenIsDead/budge/compare/v1.9.0...v1.10.0) (2025-10-27)


### Features

* add total in and out to transaction search ([aa391bf](https://github.com/TheQueenIsDead/budge/commit/aa391bf7e3fcc87b89972f5371609c0dde5fa2f7))

## [1.9.0](https://github.com/TheQueenIsDead/budge/compare/v1.8.0...v1.9.0) (2025-08-25)


### Features

* add chips for all categories in the transaction list ([b105e66](https://github.com/TheQueenIsDead/budge/commit/b105e66ced85daf0ce8ef32b03705b6b16abcb27))
* add count of results and time taken to transactions ([eff7369](https://github.com/TheQueenIsDead/budge/commit/eff7369b55edb1adfbdc752aecc5eb871360d2e0))
* rework transaction searching to include all categories ([6538d7a](https://github.com/TheQueenIsDead/budge/commit/6538d7a941c7bb89d6ec373d1e84a5ca9e98046c))
* simplify transaction searching and include all categories ([1a47349](https://github.com/TheQueenIsDead/budge/commit/1a47349f9ad94cae6dde69b29298897086de41b8))


### Bug Fixes

* add 'selected' to the account option if it was selected (present in query params) ([d202f23](https://github.com/TheQueenIsDead/budge/commit/d202f239e38d9f4e07ea2a6d12b04338199f02fc))
* change akahu sync to always grab the last week in case syncs were recent and missed data not yet exposed by the bank ([295e4dc](https://github.com/TheQueenIsDead/budge/commit/295e4dc64c3b6ab7d502fa7490c866cfac4a410b))

## [1.8.0](https://github.com/TheQueenIsDead/budge/compare/v1.7.1...v1.8.0) (2025-08-18)


### Features

* bring out yer graphs ([196c969](https://github.com/TheQueenIsDead/budge/commit/196c9697f3fa19a41103ace6717734737b5e5a03))

## [1.7.1](https://github.com/TheQueenIsDead/budge/compare/v1.7.0...v1.7.1) (2025-08-05)


### Bug Fixes

* add last sync template to resolve 4XX error ([96bd901](https://github.com/TheQueenIsDead/budge/commit/96bd9016a28f8378f2f90c77efb9de0813d13ad5))
* return placeholder values for card if there is no initial sync data ([84e4027](https://github.com/TheQueenIsDead/budge/commit/84e4027baaeb25ce0e5c1f24585831d39448d508))

## [1.7.0](https://github.com/TheQueenIsDead/budge/compare/v1.6.0...v1.7.0) (2025-07-10)


### Features

* add tooltips to percentage changes and fixup weird logic resulting from diffs of negative values ([8f60ff1](https://github.com/TheQueenIsDead/budge/commit/8f60ff1dc8589fc2b3cae0b765feb8045a23c2f0))
* add tooltips to percentage changes and fixup weird logic resulting from diffs of negative values ([0b208df](https://github.com/TheQueenIsDead/budge/commit/0b208df8b6ae425609621d09106d613b2ee7f9ef))

## [1.6.0](https://github.com/TheQueenIsDead/budge/compare/v1.5.0...v1.6.0) (2025-07-01)


### Features

* change settings to update last sync timestamp when sync successful ([bb74831](https://github.com/TheQueenIsDead/budge/commit/bb74831a066d97584d710c8747413c130f101293))
* settings refresh ([cf5733e](https://github.com/TheQueenIsDead/budge/commit/cf5733ea4403f21a3d7a70ce33b4b7437c3e6778))

## [1.5.0](https://github.com/TheQueenIsDead/budge/compare/v1.4.1...v1.5.0) (2025-06-30)


### Features

* better identify transfers between user owned accounts ([d6efef7](https://github.com/TheQueenIsDead/budge/commit/d6efef7880f06a8f6bfe92d15ebdd138279193c6))
* categorize payments where there is an equal in and out value at the same time as a transfer ([57c2683](https://github.com/TheQueenIsDead/budge/commit/57c2683fd872c073782def238a2248db2fcccae4))


### Bug Fixes

* format top spend as currency instead of percentage ([a5fd836](https://github.com/TheQueenIsDead/budge/commit/a5fd83697090a295f491893abfd957af6974c723))

## [1.4.1](https://github.com/TheQueenIsDead/budge/compare/v1.4.0...v1.4.1) (2025-05-13)


### Bug Fixes

* return err in cache middleware ([f36d968](https://github.com/TheQueenIsDead/budge/commit/f36d968e33406079f4e317abec14d8d7769f52c5))

## [1.4.0](https://github.com/TheQueenIsDead/budge/compare/v1.3.0...v1.4.0) (2025-05-13)


### Features

* add page caching based on last integration sync ([b4b9578](https://github.com/TheQueenIsDead/budge/commit/b4b9578413154440aa8eea4d11afe7832b33dc67))
* add page caching based on last integration sync ([a1d9b5c](https://github.com/TheQueenIsDead/budge/commit/a1d9b5c6e964eefe586d933aed2b6da9c4af642f))
* add the ability to search transactions ([5023208](https://github.com/TheQueenIsDead/budge/commit/50232082978e38848ef6f0c9d75e1f865a9c85fc))

## [1.3.0](https://github.com/TheQueenIsDead/budge/compare/v1.2.0...v1.3.0) (2025-05-12)


### Features

* add in global format func for showing floats as comma-delimited strings ([e9e08c3](https://github.com/TheQueenIsDead/budge/commit/e9e08c3065a997cbbf9ac26f759154dc01e6c227))
* add wip new layout with mock data for dashboard ([57bc48e](https://github.com/TheQueenIsDead/budge/commit/57bc48eb62bd4ccb00eadcf3e540065533aa3bbb))
* rename home to dashboard and hook up total balance ([41874c9](https://github.com/TheQueenIsDead/budge/commit/41874c99d3d665af4227d891667858fc802ef41f))
* ui refresh ([ba5cef6](https://github.com/TheQueenIsDead/budge/commit/ba5cef6ad7bc2212690a63ae75130dda5dd3bad4))
* upgrade bootstrap from 5.3.3 to 5.3.6 ([30a8c81](https://github.com/TheQueenIsDead/budge/commit/30a8c81cff985ac2601632a586efe1490a4f8e26))
* use helpers for currency and percentage display ([88ffc7f](https://github.com/TheQueenIsDead/budge/commit/88ffc7f77f44338fd5b09b794bfb065fecd5e1b8))
* wire in values for most dashboard values and hide less used pages from nav ([c30fb5c](https://github.com/TheQueenIsDead/budge/commit/c30fb5c73247e192b076df75fc4185b67f7ef08a))


### Bug Fixes

* format currency to 2dp ([1284edf](https://github.com/TheQueenIsDead/budge/commit/1284edf90c962d6819074a24c4a1128ee9678702))
* mark transactions as salary if they contain "salary" in the description ([b38f8ee](https://github.com/TheQueenIsDead/budge/commit/b38f8ee6a03bd465676f5b0982b72e03e0bffe67))
* stop page from jumping on nav click by disabling boost scroll into view ([30fd50a](https://github.com/TheQueenIsDead/budge/commit/30fd50af7f12587014b96ff150c0fdf2fc444cde))

## [1.2.0](https://github.com/TheQueenIsDead/budge/compare/v1.1.0...v1.2.0) (2025-05-04)


### Features

* consolidate home page reporting ([1ffbb7e](https://github.com/TheQueenIsDead/budge/commit/1ffbb7eb6c4618608aaebb3876293a1daf71a6e2))
* consolidate transactions by category and timeseries chart in home ([ce93e5a](https://github.com/TheQueenIsDead/budge/commit/ce93e5a2223ab054e3980b88cee4cf9c2c1ebc5f))


### Bug Fixes

* persist akahu settings on register if not present ([af267e8](https://github.com/TheQueenIsDead/budge/commit/af267e8e424cfec9a431b8d7bca49c917b8db710))

## [1.1.0](https://github.com/TheQueenIsDead/budge/compare/v1.0.0...v1.1.0) (2025-03-30)


### Features

* spend by category reporting ([4f019d7](https://github.com/TheQueenIsDead/budge/commit/4f019d700120022ab4ec2eb160001fc34939239a))

## 1.0.0 (2025-03-30)


### Features

* accounts and bank detection based on CSV file headers ([7dde1b6](https://github.com/TheQueenIsDead/budge/commit/7dde1b61402d9c6128619c81bebe39d52efb119b))
* add 4XX error page ([a736af8](https://github.com/TheQueenIsDead/budge/commit/a736af8ae55286fae86c7b5c4e28a1b97938b98d))
* add ability to delete inventory items and add humanised 'added' time ([8971e7a](https://github.com/TheQueenIsDead/budge/commit/8971e7a7a0cc6770b65eee1267271ff877902047))
* add back-reference to account in transaction in order to display name correctly ([e7cf8f3](https://github.com/TheQueenIsDead/budge/commit/e7cf8f380bd02ea0219da98cf1c916bdcc480b3e))
* add budget db with sqlite ([7798ef0](https://github.com/TheQueenIsDead/budge/commit/7798ef0f23a649ff69cb77688028a53b405ec4a0))
* add category to merchant based on enriched transaction data ([400d2e2](https://github.com/TheQueenIsDead/budge/commit/400d2e2eaee66847642b83ce3fc7977383faaf1a))
* add charts for account balance over time (mock data ([40d791e](https://github.com/TheQueenIsDead/budge/commit/40d791eea7a2de6e78a6aef5422a4cc9defd42c5))
* add docker publish workflow ([adee90b](https://github.com/TheQueenIsDead/budge/commit/adee90ba6b20f1356818f6a18721097cf2180ec3))
* add Dockerfile ([f022f23](https://github.com/TheQueenIsDead/budge/commit/f022f230df110053a4c49adea5cd4bb9602fa31e))
* add form for creating new inventory items ([6aa0453](https://github.com/TheQueenIsDead/budge/commit/6aa0453a10b562f2a2191db16263f2dfc6efecea))
* add hello world echo server ([d706fdf](https://github.com/TheQueenIsDead/budge/commit/d706fdf3976895ae86cca61ff07a4e2dd790b33e))
* add incoming vs outgoing summary and chart ([a72cca8](https://github.com/TheQueenIsDead/budge/commit/a72cca8f86d5b1f601b52fde9e61c081020ae31e))
* add inventory management to track purchases ([928d2a0](https://github.com/TheQueenIsDead/budge/commit/928d2a06fbf24e995ebd03d7473f2a1daf566239))
* add logging middleware to display errors in echo ([3afe14e](https://github.com/TheQueenIsDead/budge/commit/3afe14e279f1a9f543070cd48deb118ef37f981a))
* add readme with cute budgie ([ee57ef1](https://github.com/TheQueenIsDead/budge/commit/ee57ef10e8335c01ffd11b1ba616dc593f587d1b))
* add settings page to show akahu config ([125c6ff](https://github.com/TheQueenIsDead/budge/commit/125c6ffff5b354ba7c348c2f1eb9d732cba078db))
* add sum of incoming and outgoing to home page ([a4c9cec](https://github.com/TheQueenIsDead/budge/commit/a4c9cecca14c06bf9281a471ac132fc59af1be1b))
* add svg spinner to akahu sync ([062ea0b](https://github.com/TheQueenIsDead/budge/commit/062ea0b02bd49e0efcd9431d19131f6bbb44b5e4))
* add theme ([9a9471f](https://github.com/TheQueenIsDead/budge/commit/9a9471fb802170fd6a2211cebb7503a1c21617d2))
* allow file uploads for CSV's ([fe0d988](https://github.com/TheQueenIsDead/budge/commit/fe0d988a3e99dc5f73a88f4931cde52182a2bdd6))
* allow setting bolt db filepath ([e221926](https://github.com/TheQueenIsDead/budge/commit/e2219268a20b1a1439610873c6effdf4d9db1619))
* allow users to configure akahu api token via UI ([cdc08cc](https://github.com/TheQueenIsDead/budge/commit/cdc08cc9bcab21ee8dd833edeeb7e566400c9d4d))
* calculate balance over time given transactions ([c1ceada](https://github.com/TheQueenIsDead/budge/commit/c1ceada5051d50a70d043c9b5e8fc5b85df31e69))
* consolidate boilerplate store functions into generics ([0a06ce7](https://github.com/TheQueenIsDead/budge/commit/0a06ce751f733a701df4bc17247b6fd2bb786f6b))
* consolidate boilerplate store functions into generics ([a9b8b2a](https://github.com/TheQueenIsDead/budge/commit/a9b8b2a324fb296a06e6fdbdb4050169e9e1d347))
* convert list routes to boltdb ([50fcae5](https://github.com/TheQueenIsDead/budge/commit/50fcae50c9420d52443ed5264dfcc290fc61e15c))
* correctly link transactions to accounts ([dabf119](https://github.com/TheQueenIsDead/budge/commit/dabf119ef2555a0b01081301153dad6901dd3fd2))
* display a count of accounts, merchants, and transactions ([3a1a4aa](https://github.com/TheQueenIsDead/budge/commit/3a1a4aa3c5cd103311362f4d2b6f667d6fa04e3c))
* display a count of accounts, merchants, and transactions ([5f910e2](https://github.com/TheQueenIsDead/budge/commit/5f910e287bb0e928a47360f5835edbd8bb096d62))
* display most recent transactions first ([7e3046f](https://github.com/TheQueenIsDead/budge/commit/7e3046f61aa3ec0dbc862c32939fc937ffd0f54d))
* display parsed kiwibank CSV back to user on upload ([f8ff891](https://github.com/TheQueenIsDead/budge/commit/f8ff891c6cdb8890be72a9758f0a83c43cc70f43))
* drop synced data (transactions, accounts, and merchants) via the settings ([e2b9028](https://github.com/TheQueenIsDead/budge/commit/e2b9028e578187e59471b6f3af09fa84e50a2194))
* enumerate budget items in template ([35d8362](https://github.com/TheQueenIsDead/budge/commit/35d8362f098f4dee369252eda4f83f46d5df0e81))
* filter transactions by account ([ac1930c](https://github.com/TheQueenIsDead/budge/commit/ac1930c002028de47682c77c3dcea8898511fada))
* Filter transactions by account ([bf3fb44](https://github.com/TheQueenIsDead/budge/commit/bf3fb4459d80936e928faa36bb19bf9721547e1f))
* generify import function by identifying the bank based on CSV header ([11703c0](https://github.com/TheQueenIsDead/budge/commit/11703c003550f23b63cfebb61e3606a6f9352658))
* get CSV imports working again for all collections ([f4f067b](https://github.com/TheQueenIsDead/budge/commit/f4f067bb870e748230610edef6893c614d7e63fa))
* hash models in order to bulk upload accounts and merchants ([4d6c58b](https://github.com/TheQueenIsDead/budge/commit/4d6c58be7ec86464d6b2ec05b110cba44e8cd624))
* hx boost count cards to mitigate reload ([9ecd8e3](https://github.com/TheQueenIsDead/budge/commit/9ecd8e351215a6e8e8891a7fbb7472e4edeea6e9))
* implement ability to save merchant details ([dd40d35](https://github.com/TheQueenIsDead/budge/commit/dd40d35fbdf3a52698062965cd361c2d5afa263a))
* integrate akahu, sync models to budge db, and add settings page for config ([aaffb2b](https://github.com/TheQueenIsDead/budge/commit/aaffb2b1dd3f8e2a2776a35d4fe857751d4e4d51))
* keep track of last akahu sync and only sync subsequent data ([23a8b46](https://github.com/TheQueenIsDead/budge/commit/23a8b469ebd570c3077432f5b969650a471076b9))
* keep track of last akahu sync and only sync subsequent data ([21bd630](https://github.com/TheQueenIsDead/budge/commit/21bd630ec8f7ded94f4cff49324d1e887db1ae2f))
* make navbar elements active when clicked with hyperscript, and enable SPA with HX boost ([67b828c](https://github.com/TheQueenIsDead/budge/commit/67b828c32af8f5b73862569518c3e0e7ec8dee84))
* make navbar elements active when clicked with hyperscript, and enable SPA with HX boost ([3a9a8b5](https://github.com/TheQueenIsDead/budge/commit/3a9a8b5b2abb20d27efa699e0d2572df5f9739c0))
* new inventory item creation and listing ([5f7f70f](https://github.com/TheQueenIsDead/budge/commit/5f7f70fd90384ece0e5a3280a6bb465d8fe82e55))
* paginate akahu transactions ([2d5e156](https://github.com/TheQueenIsDead/budge/commit/2d5e1566ccd7f203c5035cff09811cebf39f82dc))
* paginate akahu transactions ([85af9c9](https://github.com/TheQueenIsDead/budge/commit/85af9c95a84c60a717f3d2e2b93f9b415775b666))
* pass accounts to transaction page for filtering ([2622433](https://github.com/TheQueenIsDead/budge/commit/262243336076baeeddba937b1f221cc6d40332c0))
* persist merchants on upload and render pages on top of a base layout ([2964442](https://github.com/TheQueenIsDead/budge/commit/2964442c43ae2b0a6b0f47283aca55fec1049e31))
* persist transactions if they are new ([dc9e9c6](https://github.com/TheQueenIsDead/budge/commit/dc9e9c6786c55f3d09ab6520000b47dd575e4e0f))
* persist transactions if they are new ([6d474dc](https://github.com/TheQueenIsDead/budge/commit/6d474dc4d47d06cefa6ffa9d3fa09a4e2028ceaa))
* prettify overview page layout and add top merchants ([103e6d5](https://github.com/TheQueenIsDead/budge/commit/103e6d53de9dce80427d3e07f063c774dcb9553c))
* reenable list endpoints, upload, and index counts ([b1e7516](https://github.com/TheQueenIsDead/budge/commit/b1e75168a8661cbb21f4eaa096e896edb1ca9a9d))
* reformat transactions to store debit vs credit int and value as integer ([7df62e1](https://github.com/TheQueenIsDead/budge/commit/7df62e18a7ca3d5407fc4fac04734f037a663d2c))
* reformat transactions to store debit vs credit int and value as integer ([3d1f735](https://github.com/TheQueenIsDead/budge/commit/3d1f735f113f41dd713915e9fc0b4e56c0d7edb6))
* remove javascript from settings page ([2a8c09d](https://github.com/TheQueenIsDead/budge/commit/2a8c09df001ebffd0c603e9d5a406197369eb649))
* remove padding from donut chart and make responsive ([c5cf097](https://github.com/TheQueenIsDead/budge/commit/c5cf097291c47c11cf991a1d45d92ead7a07561d))
* remove padding from donut chart and make responsive ([b26a3e3](https://github.com/TheQueenIsDead/budge/commit/b26a3e3fbf8a8d044097d8d34a74ed5536bf4b5a))
* retrieve and format timeseries data for spend over time with configurable periods ([6101693](https://github.com/TheQueenIsDead/budge/commit/6101693b4e28ff840f4c198e7b5ddf62b3d9f01a))
* retrieve chart data via individual routes and standardise canvas creation via gohtml ([aebbeeb](https://github.com/TheQueenIsDead/budge/commit/aebbeebd427e58bed4d649684b1ecf70c5c0031d))
* revamped overview ([b849198](https://github.com/TheQueenIsDead/budge/commit/b84919875ac3ce4670f86b759ec5440ee1d44ae3))
* show balance of account ([2d192db](https://github.com/TheQueenIsDead/budge/commit/2d192dbf64f6d84bceec859d8d711687aae18a38))
* spend over time ([eac6ccd](https://github.com/TheQueenIsDead/budge/commit/eac6ccd36cd543dc2c0a04b30d54057c0ae10105))
* store accounts as they are encountered ([c546385](https://github.com/TheQueenIsDead/budge/commit/c546385d5e7e7c4176f4578e34e5f38e1e398936))
* store accounts as they are encountered ([ce83b1f](https://github.com/TheQueenIsDead/budge/commit/ce83b1f714c7d9c641b26b70f2f7781adf52e5e9))
* sync akahu merchants ([ab63b01](https://github.com/TheQueenIsDead/budge/commit/ab63b011915bb90b873075c63944f6e5335586e9))
* sync categories for transactions from Akahu ([385c53d](https://github.com/TheQueenIsDead/budge/commit/385c53d12d2a1185d96ca3b6abcafe2ad830e5e6))
* tidy up merchant names by removing common guff ([df61b20](https://github.com/TheQueenIsDead/budge/commit/df61b20dc48731b7844ab91b0bb07128ca4d7d70))
* use merchant data from transactions if available ([3c68484](https://github.com/TheQueenIsDead/budge/commit/3c68484cc9dadd03b99179d7c07fced1dc9de4ee))
* working toast handler and error reporting on failed settings save ([bd644ea](https://github.com/TheQueenIsDead/budge/commit/bd644ea6272758d0623e11c76a5172edfbefe42f))


### Bug Fixes

* add --yes to cosign ([02a2e41](https://github.com/TheQueenIsDead/budge/commit/02a2e412a60a91b89c1f1731c595c08c85a19898))
* add --yes to cosign ([d5a7830](https://github.com/TheQueenIsDead/budge/commit/d5a783017cfea7f7635ee90bb342796dffa54431))
* button layout in akahu config now behaves as intended ([081b9d1](https://github.com/TheQueenIsDead/budge/commit/081b9d1dcbe3cd3bff178b4cf19b177b940fb8e3))
* delete inventory item cleanly and remove client side ([5874805](https://github.com/TheQueenIsDead/budge/commit/587480591d39889077adb36d3af14d4112072b09))
* fix navbar toggler by moving to bootstrap 5 conventions and fix margins ([a96940e](https://github.com/TheQueenIsDead/budge/commit/a96940e7e6b019e48e5b8a87bcf33390b43dfce3))
* fix navbar toggler by moving to bootstrap 5 conventions and fix margins ([9fbccca](https://github.com/TheQueenIsDead/budge/commit/9fbccca0c8e1603d5ab3b6e2d4fd8c3469e861d1))
* incorrectly named entries on settings form ([d5735bd](https://github.com/TheQueenIsDead/budge/commit/d5735bd23211bcd42bff528c261ea8d8f554132d))
* lazy load akahu connected accounts in the settings page ([ec51807](https://github.com/TheQueenIsDead/budge/commit/ec51807591885f179f13380646c3b08cee03e8ba))
* no longer nukes Save button on execution ([ba94475](https://github.com/TheQueenIsDead/budge/commit/ba94475370855d4d3689fb57e8aef48987aef6c8))
* no longer nukes Sync button on execution ([793d623](https://github.com/TheQueenIsDead/budge/commit/793d623f12137b815d32bf2cec905036fdf683ee))
* resolve 'SyntaxError: redeclaration of const' error on navigating back to index via htmx ([44acd6a](https://github.com/TheQueenIsDead/budge/commit/44acd6a17037416e084dd6a22d2bf236aa7f5a9f))
* resolve build in example akahu script ([277c7b6](https://github.com/TheQueenIsDead/budge/commit/277c7b683f7ab6eb7d7620c5d49ae410ee3083a5))
* resolve nil pointer issue by initialising logger correctly ([5cb6d51](https://github.com/TheQueenIsDead/budge/commit/5cb6d5137affddcd06e792c627f27fd765cd750c))
* typo in theme.js ref ([f55ac52](https://github.com/TheQueenIsDead/budge/commit/f55ac522629da44d7a6636e7470982add48a1288))
