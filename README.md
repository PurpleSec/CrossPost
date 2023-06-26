# CrossPost

Post Mastodon Posts to Twitter easily!

CrossPost will allows you to create a simple poster from Mastodon->Twitter.

## Instructions

To run CrossPost, you will need Twitter and Mastodon API keys.

### Twitter

For Twitter you need a Twitter developer application. This can be done at
<https://developer.twitter.com>. You will need to create a Project and assign an
application to it. The Free tier for the project is completely fine.

You will need to generate an `Access Token and Secret` with `Read and Write` access.
This can be done in the Application settings under `User authentication settings`
by clicking `Set up` and making sure **at least** `Read and Write` is the authentication
level.

Save the `API Key and Secret` and `Access Token and Secret` values.

### Mastodon

This is pretty easy. You will need to log into the web interface of the instance
you're on and go to your Profile preferences (by clicking `Preferences`).

Click on the `Development` tab on the bottom left. Create an application and make
sure the `read:accounts` and `read:statuses` permissions are selected.

Save the `Client key`, `Client secret` and `Your access token` values.

## Configuration

Copy a version of the `Example Configuration` below and fill in the values you
got from the instructions above.

Make sure to install the python requirements for CrossPost by using the
`pip install -r requirements.txt` while in the CrossPost directory.

Now all you have to do is run it using the `crosspost.py <config_file>` (the
"<config_file>" value should be replaced with the path to the config file you
created).

# Example Configuration

Here is an example configration that covers two separate accounts.

```json
{
    "accounts": [
        {
            "mastodon": {
                "server": "https://<instance_url>",
                "client_id": "client_id_value",
                "client_secret": "client_secret_value",
                "access_token": "access_token_value"
            },
            "twitter": {
                "consumer_key": "consumer_key_value",
                "consumer_secret": "consumer_secret_value",
                "access_token": "access_token_value",
                "access_secret": "access_secret_value"
            }
        },
        {
            "mastodon": {
                "server": "https://<instance_url>",
                "client_id": "client_id_value",
                "client_secret": "client_secret_value",
                "access_token": "access_token_value"
            },
            "twitter": {
                "consumer_key": "consumer_key_value",
                "consumer_secret": "consumer_secret_value",
                "access_token": "access_token_value",
                "access_secret": "access_secret_value"
            },
            "prefix": "<prefix_short_url>/post/"
        }
    ]
}
```
