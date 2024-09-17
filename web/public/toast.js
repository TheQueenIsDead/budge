const showToast =function (evt) {
    const toastLiveExample = document.getElementById('toast')
    const toastText = document.getElementById('toast-body')
    const toastBootstrap = bootstrap.Toast.getOrCreateInstance(toastLiveExample)
    toastText.innerText = evt.detail.value
    console.log(evt.detail.value)
    toastBootstrap.show()
}
document.body.addEventListener("error", showToast)