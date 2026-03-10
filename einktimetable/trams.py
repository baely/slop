import datetime
import requests
import zoneinfo

STOP_NUMBER = 1917

templ = """<style>
@font-face {{
font-family: 'VHS Gothic';src: url('/static/vhs-gothic.ttf') format('truetype');
}}
.inverse {{
background-color: #000; color: #fff;
}}
.border {{
border: 2px solid black; margin: 0; display: inline-block; width: 200px; padding: 16px; 
}}
</style>
<div style="font-family:'VHS Gothic';font-size: 32px;">
<div class="inverse" style="padding: 8px;">Next Trams</div>
<span class="border" style="">{}</span><span class="border" style="display: inline-block; width: 200px;">{}</span>
<span class="border" style="height:176px; vertical-align: top;">{}</span><span class="border" style="height:176px; vertical-align: top;">{}</span>
</div><div class="inverse" style="display:inline-block;position:absolute;top:18px;left:285px;font-family:'VHS Gothic';font-size:16px;">{}</div>"""



api = f"https://tramtracker.com.au/Controllers/GetNextPredictionsForStop.ashx?stopNo={STOP_NUMBER}&routeNo=0&isLowFloor=false"

melbourne_tz = zoneinfo.ZoneInfo("Australia/Melbourne")

def generate_page():
    resp = requests.get(api)
    body = resp.json()

    routes = {}

    dt_now = datetime.datetime.now(melbourne_tz)

    for resp in body["responseObject"]:
        if resp["RouteNo"] not in routes.keys():
            routes[resp["RouteNo"]] = []
        pred_time = resp["PredictedArrivalDateTime"]
        pred_timestamp = int(pred_time[6:-7]) // 1000
        dt = datetime.datetime.fromtimestamp(pred_timestamp, melbourne_tz)
        delta = dt - dt_now
        min_until = delta.total_seconds() // 60

        if min_until < 60:
            time_display = f"{int(min_until)}"
        # elif dt.date() == dt_now.date():
        elif (dt - dt_now).seconds <= 24 * 60 * 60:
            time_display = dt.strftime("%H:%M")
        elif (dt.date() - dt_now.date()).days <= 7:
            time_display = dt.strftime("%a %H:%M")
        else:
            time_display = dt.strftime("%d/%m %H:%M")

        if "TramClass" in resp.keys() and len(resp["TramClass"]) > 0:
            time_display += "&nbsp;" * max(4 - len(time_display), 1) + resp["TramClass"][0]

        routes[resp["RouteNo"]].append(time_display)

    keys = list(routes.keys())[:2]
    times = [
        "<br>".join(routes[keys[0]]),
        "<br>".join(routes[keys[1]]),
    ]

    print(keys, times)

    return templ.format(
        *keys, *times,
        dt_now.strftime("%H:%M:%S")
    )