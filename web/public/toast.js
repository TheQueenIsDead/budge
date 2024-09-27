const showToast =function (evt) {
    const toastLiveExample = document.getElementById('toast')
    const toastBody = document.getElementById('toast-body')
    const toastHeader = document.getElementById('toast-header')
    const toastBootstrap = bootstrap.Toast.getOrCreateInstance(toastLiveExample)
    toastBody.innerText = evt.detail.message
    toastHeader.innerText = evt.detail.level
    console.log(evt.detail.message)
    toastBootstrap.show()
}
document.body.addEventListener("toast", showToast)