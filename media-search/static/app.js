var nodes = {
	searchInput: document.querySelector('.js-search-input'),
	searchButton: document.querySelector('.js-search-button'),
	searchSection: document.querySelector('.js-search-section'),
};

var isSearching = false;
var searchResults = [];

// Basic HTTP request method
function sendRequest(object) {
	var successCallback = object.successCallback,
		errorCallback = object.errorCallback,
		method = object.method.toUpperCase(),
		data = object.data,
		url = object.url,
		xhr = new XMLHttpRequest();

	var usesJson = (method === 'POST' || method === 'PUT');

	xhr.open(method, url);
	xhr.setRequestHeader('Access-Control-Allow-Origin', '*');

	if (usesJson) {
		xhr.setRequestHeader('Content-Type', 'application/json');
	}

	xhr.onreadystatechange = function() {
		if (xhr.readyState === 4) {
			if (xhr.status === 200) {
				return successCallback(JSON.parse(xhr.responseText));
			} else {
				return errorCallback(xhr.status);
			}
		}
	}

	if (usesJson) {
		xhr.send(JSON.stringify(data));
	} else {
		xhr.send();
	}
}

function collapseSearchSection() {
	if (nodes.searchSection.classList.contains('search-section-collapsed')) {
		return;
	}

	nodes.searchSection.classList.add('search-section-collapsed');
}

function onSearchBegin() {
	collapseSearchSection();
	isSearching = true;
}

function onSearchEnd() {
	isSearching = false;
}

function successCallback(response) {

}

function errorCallback() {
	alert('Something went wrong.');
}

function onSearchClick() {
	if (isSearching) {
		return;
	}

	onSearchBegin();

	var query = nodes.searchInput.value.trim();

	sendRequest({
		method: 'POST',
		data: {"q": query},
		url: 'http://localhost:9778/search',
		successCallback: successCallback,
		errorCallback: errorCallback
	});
}

nodes.searchButton.addEventListener('click', onSearchClick)

